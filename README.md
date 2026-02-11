# Mental Math Arena

A real-time multiplayer mental math game where players compete against each other to solve arithmetic problems.

## Architecture

The project consists of two main components:
*   **Backend**: A Go server handling matchmaking, game logic, and real-time communication via WebSockets.
*   **Mobile**: A React Native (Expo) application for the client interface.

### Tech Stack
*   **Backend**: Go (Golang), Gorilla WebSocket, Redis (for matchmaking queue and state)
*   **Mobile**: React Native, Expo, TypeScript
*   **Infrastructure**: Redis

## Prerequisites

*   Go 1.22+
*   Node.js & npm/yarn
*   Redis server running locally on default port (6379)

## Getting Started

### Backend

1.  Navigate to the backend directory:
    ```bash
    cd backend
    ```

2.  Install dependencies:
    ```bash
    go mod download
    ```

3.  Run the server:
    ```bash
    go run cmd/server/main.go
    ```
    The server will start on port 8080 (default).

### Mobile App

1.  Navigate to the mobile directory:
    ```bash
    cd mobile/MathArena
    ```

2.  Install dependencies:
    ```bash
    npm install
    ```

3.  Start the Expo development server:
    ```bash
    npm start
    ```
    Use the Expo Go app on your phone or an emulator to scan the QR code and run the app.

## Testing

### Backend Tests
To run all backend tests:
```bash
cd backend
go test ./...
```

### Load Testing
A load testing tool is included in `backend/cmd/loadtest`.
```bash
go run backend/cmd/loadtest/main.go --users 50 --duration 30s
```

## Project Structure
*   `backend/cmd`: Entry points for applications
*   `backend/internal`: Private application code (game logic, matchmaking, etc.)
*   `backend/pkg`: Public libraries
*   `mobile/MathArena`: React Native application source
