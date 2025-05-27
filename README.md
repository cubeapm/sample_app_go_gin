# Sample Go Project with Gin and GORM

A sample Go application built with the Gin web framework and GORM for MySQL database connectivity. This project demonstrates best practices for building REST APIs in Go.

## Features

- **Gin Web Framework**: Fast HTTP web framework for Go
- **GORM**: Go ORM library with MySQL support
- **Health Check API**: Basic health endpoint to check service status
- **Internal Health Check**: Endpoint that makes internal HTTP calls using `net/http`
- **Database Integration**: MySQL connectivity with auto-migration
- **CORS Support**: Cross-Origin Resource Sharing enabled
- **Environment Configuration**: Configurable via environment variables

## Project Structure

```
.
├── main.go          # Main application file
├── go.mod           # Go module file
├── go.sum           # Go module checksum
└── README.md        # This file
```

## Prerequisites

- Go 1.23 or later
- MySQL 8.0 or later
- Git (for version control)

## Installation

1. **Clone or create the project:**

   ```bash
   git clone <repository-url>
   cd sample-gin-project
   ```

2. **Install dependencies:**

   ```bash
   go mod tidy
   ```

### Example `.env` file:

```env
PORT=8080
DATABASE_URL=root:yourpassword@tcp(localhost:3306)/testdb?charset=utf8mb4&parseTime=True&loc=Local
```

## Running the Application

1. **Start MySQL server** (make sure it's running on port 3306)

2. **Run the application:**

   ```bash
   go run main.go
   ```

3. **The server will start on port 8080 by default**
   ```
   Server starting on port 8080
   Health endpoint: http://localhost:8080/health
   Internal check endpoint: http://localhost:8080/internal-check
   ```

## API Endpoints

### 1. Health Check

- **URL**: `/health`
- **Method**: `GET`
- **Description**: Returns service health status and database connectivity
- **Response**:
  ```json
  {
    "status": "healthy",
    "timestamp": "2024-01-15T10:30:00Z",
    "database": "connected"
  }
  ```

### 2. Internal Health Check

- **URL**: `/internal-check`
- **Method**: `GET`
- **Description**: Makes an internal HTTP call to `/health` endpoint using `net/http`
- **Response**:
  ```json
  {
    "status": "healthy",
    "timestamp": "2024-01-15T10:30:00Z",
    "internal_health": {
      "status": "healthy",
      "timestamp": "2024-01-15T10:30:00Z",
      "database": "connected"
    }
  }
  ```

## Package Versions

This project uses the latest stable versions of all dependencies:

- `github.com/gin-gonic/gin` v1.10.1
- `gorm.io/gorm` v1.30.0
- `gorm.io/driver/mysql` v1.5.7

To check for updates:

```bash
go list -u -m all
```

## Development

### Building the application:

```bash
go build -o sample-gin-project main.go
```

### Running tests:

```bash
go test ./...
```
