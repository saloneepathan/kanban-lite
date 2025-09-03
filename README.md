# Kanban Lite – A Mini Trello Clone in Go

Kanban Lite is a minimal Trello-like board app built with **Go**, featuring:
- **Boards, Lists, and Cards** CRUD
- **Real-time updates** via Server-Sent Events (SSE)
- **JSON file persistence** (no database needed)
- Simple REST API ready for frontend or CLI integration

---

## Features

- Create **boards** with multiple **lists** and **cards**
- **Move cards** between lists with drag-and-drop support (frontend ready)
- **Real-time streaming** using SSE for instant updates
- **Lightweight persistence** using local JSON file
- **Docker-ready** for easy deployment

---

## Tech Stack

- **Go** for backend (using `chi` router)
- **Server-Sent Events (SSE)** for real-time updates
- **CORS** enabled for frontend integration
- **Docker** for containerization

---

## Setup

### 1. Clone the repo
```bash
git clone https://github.com/your-username/kanban-lite.git
cd kanban-lite
````

### 2. Install dependencies

```bash
go mod tidy
```

### 3. Run the server

```bash
go run main.go
```

Server starts at:

```
http://localhost:8080
```

---

## API Endpoints

### Health Check

```bash
GET /health
```

Response: `ok`

---

### Boards

| Method | Endpoint          | Description       |
| ------ | ----------------- | ----------------- |
| GET    | /boards           | List all boards   |
| POST   | /boards           | Create new board  |
| GET    | /boards/{boardID} | Get board details |

Example – Create Board:

```bash
curl -X POST http://localhost:8080/boards \
  -H "Content-Type: application/json" \
  -d '{"title":"Project Alpha"}'
```

---

### Lists

| Method | Endpoint                | Description            |
| ------ | ----------------------- | ---------------------- |
| POST   | /boards/{boardID}/lists | Create list in a board |

Example – Create List:

```bash
curl -X POST http://localhost:8080/boards/BOARD_ID/lists \
  -H "Content-Type: application/json" \
  -d '{"title":"To Do"}'
```

---

### Cards

| Method | Endpoint                               | Description             |
| ------ | -------------------------------------- | ----------------------- |
| POST   | /boards/{boardID}/lists/{listID}/cards | Create card in a list   |
| POST   | /boards/{boardID}/move                 | Move card between lists |

Example – Create Card:

```bash
curl -X POST http://localhost:8080/boards/BOARD_ID/lists/LIST_ID/cards \
  -H "Content-Type: application/json" \
  -d '{"title":"First Task", "description":"Test task"}'
```

---

### Real-time SSE Events

Connect to SSE endpoint:

```bash
curl http://localhost:8080/boards/BOARD_ID/events
```

Events fire when:

* A list is created
* A card is created
* A card is moved

---

## Docker Setup

### Build the image

```bash
docker build -t kanban-lite .
```

### Run the container

```bash
docker run -p 8080:8080 -v $(pwd)/data:/app/data kanban-lite
```

---

## Data Storage

* All data is stored in `./data/kanban.json`
* Automatically created if it doesn’t exist
* Persisted even after server restarts

---

## Example Flow

1. **Create Board** → Get `BOARD_ID`
2. **Create List** → Get `LIST_ID`
3. **Create Card** → Assign to `LIST_ID`
4. **Move Card** → Move between lists
5. **Open SSE** → See real-time events

---

## Roadmap

* [ ] Add JWT authentication
* [ ] Add minimal React UI for drag-and-drop
* [ ] Add tests for API endpoints
* [ ] Add graceful shutdown with context

---

## License

MIT License © 2025 Your Name