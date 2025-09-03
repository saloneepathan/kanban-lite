package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

// ==== Data Models ====

type Board struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Lists  []List `json:"lists"`
	Events int64  `json:"events"` // monotonically increasing event id
}

type List struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Position int    `json:"position"`
	Cards    []Card `json:"cards"`
}

type Card struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Position    int        `json:"position"`
	Due         *time.Time `json:"due,omitempty"`
}

// ==== In-memory store with JSON persistence ====

type Store struct {
	mu     sync.RWMutex
	path   string
	boards map[int64]*Board
	// streams: boardID -> list of subscriber channels
	streams map[int64]map[chan []byte]struct{}
}

func NewStore(path string) *Store {
	return &Store{path: path, boards: map[int64]*Board{}, streams: map[int64]map[chan []byte]struct{}{}}
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	return dec.Decode(&s.boards)
}

func (s *Store) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tmp := s.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s.boards); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, s.path)
}

// ---- Event broadcasting (SSE) ----
func (s *Store) broadcast(boardID int64, typ string, data any) {
	msg := struct {
		Type string `json:"type"`
		Data any    `json:"data"`
	}{typ, data}
	b, _ := json.Marshal(msg)

	s.mu.RLock()
	subs := s.streams[boardID]
	for ch := range subs {
		select {
		case ch <- b:
		default: /* drop if slow */
		}
	}
	s.mu.RUnlock()
}

func (s *Store) subscribe(boardID int64) (ch chan []byte, cancel func()) {
	ch = make(chan []byte, 16)
	s.mu.Lock()
	if s.streams[boardID] == nil {
		s.streams[boardID] = map[chan []byte]struct{}{}
	}
	s.streams[boardID][ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.streams[boardID], ch)
		close(ch)
		s.mu.Unlock()
	}
}

// ==== Helpers ====

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func parseID(s string) int64 {
	id, _ := strconv.ParseInt(s, 10, 64)
	return id
}

// ==== HTTP Handlers ====

type Server struct{ store *Store }

func NewServer(store *Store) *Server { return &Server{store: store} }

// Health
func (s *Server) health(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }

// Create board
func (s *Server) createBoard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
		writeJSON(w, 400, map[string]string{"error": "title required"})
		return
	}
	b := &Board{ID: time.Now().UnixNano(), Title: req.Title, Lists: []List{}}

	s.store.mu.Lock()
	s.store.boards[b.ID] = b
	s.store.mu.Unlock()
	_ = s.store.save()
	writeJSON(w, 201, b)
}

// List boards
func (s *Server) listBoards(w http.ResponseWriter, r *http.Request) {
	s.store.mu.RLock()
	out := make([]*Board, 0, len(s.store.boards))
	for _, b := range s.store.boards {
		out = append(out, b)
	}
	s.store.mu.RUnlock()
	writeJSON(w, 200, out)
}

// Create list in a board
func (s *Server) createList(w http.ResponseWriter, r *http.Request) {
	boardID := parseID(chi.URLParam(r, "boardID"))
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
		writeJSON(w, 400, map[string]string{"error": "title required"})
		return
	}

	s.store.mu.Lock()
	b := s.store.boards[boardID]
	if b == nil {
		s.store.mu.Unlock()
		writeJSON(w, 404, map[string]string{"error": "board not found"})
		return
	}
	pos := len(b.Lists)
	lst := List{ID: time.Now().UnixNano(), Title: req.Title, Position: pos, Cards: []Card{}}
	b.Lists = append(b.Lists, lst)
	b.Events++
	s.store.mu.Unlock()
	_ = s.store.save()

	s.store.broadcast(boardID, "list.created", lst)
	writeJSON(w, 201, lst)
}

// Get board with lists/cards
func (s *Server) getBoard(w http.ResponseWriter, r *http.Request) {
	boardID := parseID(chi.URLParam(r, "boardID"))
	s.store.mu.RLock()
	b := s.store.boards[boardID]
	s.store.mu.RUnlock()
	if b == nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, 200, b)
}

// Create card in a list
func (s *Server) createCard(w http.ResponseWriter, r *http.Request) {
	boardID := parseID(chi.URLParam(r, "boardID"))
	listID := parseID(chi.URLParam(r, "listID"))
	var req struct {
		Title, Description string `json:"title"`
		Due                *time.Time
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
		writeJSON(w, 400, map[string]string{"error": "title required"})
		return
	}

	s.store.mu.Lock()
	b := s.store.boards[boardID]
	if b == nil {
		s.store.mu.Unlock()
		writeJSON(w, 404, map[string]string{"error": "board not found"})
		return
	}
	var target *List
	for i := range b.Lists {
		if b.Lists[i].ID == listID {
			target = &b.Lists[i]
			break
		}
	}
	if target == nil {
		s.store.mu.Unlock()
		writeJSON(w, 404, map[string]string{"error": "list not found"})
		return
	}
	card := Card{ID: time.Now().UnixNano() + int64(rand.Intn(1000)), Title: req.Title, Description: req.Description, Position: len(target.Cards), Due: req.Due}
	target.Cards = append(target.Cards, card)
	b.Events++
	s.store.mu.Unlock()
	_ = s.store.save()

	s.store.broadcast(boardID, "card.created", card)
	writeJSON(w, 201, card)
}

// Move card between lists or reorder
func (s *Server) moveCard(w http.ResponseWriter, r *http.Request) {
	boardID := parseID(chi.URLParam(r, "boardID"))
	var req struct {
		CardID, FromListID, ToListID int64
		ToPos                        int
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad request"})
		return
	}

	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	b := s.store.boards[boardID]
	if b == nil {
		writeJSON(w, 404, map[string]string{"error": "board not found"})
		return
	}
	// find from list
	var from *List
	for i := range b.Lists {
		if b.Lists[i].ID == req.FromListID {
			from = &b.Lists[i]
			break
		}
	}
	if from == nil {
		writeJSON(w, 404, map[string]string{"error": "from list not found"})
		return
	}
	// extract card
	var c Card
	idx := -1
	for i := range from.Cards {
		if from.Cards[i].ID == req.CardID {
			c = from.Cards[i]
			idx = i
			break
		}
	}
	if idx == -1 {
		writeJSON(w, 404, map[string]string{"error": "card not found"})
		return
	}
	from.Cards = append(from.Cards[:idx], from.Cards[idx+1:]...)
	for i := range from.Cards {
		from.Cards[i].Position = i
	}
	// target list
	var to *List
	for i := range b.Lists {
		if b.Lists[i].ID == req.ToListID {
			to = &b.Lists[i]
			break
		}
	}
	if to == nil {
		writeJSON(w, 404, map[string]string{"error": "to list not found"})
		return
	}
	if req.ToPos < 0 || req.ToPos > len(to.Cards) {
		req.ToPos = len(to.Cards)
	}
	to.Cards = append(to.Cards, Card{})
	copy(to.Cards[req.ToPos+1:], to.Cards[req.ToPos:])
	to.Cards[req.ToPos] = c
	for i := range to.Cards {
		to.Cards[i].Position = i
	}
	b.Events++
	_ = s.store.save()

	s.store.broadcast(boardID, "card.moved", map[string]any{"cardId": c.ID, "toListId": to.ID, "toPos": req.ToPos})
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// SSE stream: /boards/{boardID}/events?lastEvent=123
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	boardID := parseID(chi.URLParam(r, "boardID"))
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", 500)
		return
	}

	ch, cancel := s.store.subscribe(boardID)
	defer cancel()

	// Send a ping every 25s to keep connections alive
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	writer := bufio.NewWriter(w)
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(writer, "event: message\n")
			fmt.Fprintf(writer, "data: %s\n\n", msg)
			writer.Flush()
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(writer, ": ping\n\n")
			writer.Flush()
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func main() {
	path := os.Getenv("KANBAN_DATA")
	if path == "" {
		path = "./data/kanban.json"
	}
	store := NewStore(path)
	if err := store.load(); err != nil {
		log.Fatal(err)
	}

	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "OPTIONS"},
		AllowedHeaders: []string{"*"},
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	r.Route("/boards", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			store.mu.RLock()
			out := make([]*Board, 0, len(store.boards))
			for _, b := range store.boards {
				out = append(out, b)
			}
			store.mu.RUnlock()
			writeJSON(w, 200, out)
		})
		r.Post("/", NewServer(store).createBoard)
		r.Get("/{boardID}", NewServer(store).getBoard)
		r.Post("/{boardID}/lists", NewServer(store).createList)
		r.Post("/{boardID}/cards", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "use /boards/{boardID}/lists/{listID}/cards", 404)
		})
		r.Post("/{boardID}/lists/{listID}/cards", NewServer(store).createCard)
		r.Post("/{boardID}/move", NewServer(store).moveCard)
		r.Get("/{boardID}/events", NewServer(store).events)
	})

	addr := ":8080"
	log.Printf("Kanban Lite listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
}
