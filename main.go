package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// User model for demonstration
type User struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	Name      string    `json:"name" gorm:"type:varchar(255)"`
	Email     string    `json:"email" gorm:"type:varchar(255);uniqueIndex"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Database instance
var db *gorm.DB
// var tracer apiTrace.Tracer

// HTTP client instance (reusable)
var httpClient *http.Client

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Database  string `json:"database"`
	UserCount *int64 `json:"user_count,omitempty"`
}

// InternalCheckResponse represents the response from internal health check
type InternalCheckResponse struct {
	Status           string          `json:"status"`
	Timestamp        string          `json:"timestamp"`
	InternalHealth   *HealthResponse `json:"internal_health,omitempty"`
	InternalError    string          `json:"internal_error,omitempty"`
}

func initDatabase() {
	// Database connection parameters - you can modify these or use environment variables
	dsn := "root:password@tcp(localhost:3306)/testdb?charset=utf8mb4&parseTime=True&loc=Local"
	
	// You can also use environment variables
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		dsn = dbURL
	}

	var err error
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})

	if err != nil {
		log.Printf("Failed to connect to database: %v", err)
		// For demo purposes, we'll continue without database connection
		return
	}

	// Attach OpenTelemetry plugin
	if err := db.Use(otelgorm.NewPlugin()); err != nil {
		log.Printf("Failed to attach OpenTelemetry plugin: %v", err)
		return
	}

	// Auto migrate the schema
	err = db.AutoMigrate(&User{})
	if err != nil {
		log.Printf("Failed to migrate database: %v", err)
	}

	log.Println("Database connected successfully")
}

func initHTTPClient() {
	// Initialize HTTP client with OpenTelemetry instrumentation
	httpClient = &http.Client{
		Timeout:   10 * time.Second,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
}

func healthHandler(c *gin.Context) {
	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().Format(time.RFC3339),
		Database:  "disconnected",
	}

	// Check database connection by performing a find operation
	if db != nil {
		var count int64
		// Pass the request context to propagate tracing
		if err := db.WithContext(c.Request.Context()).Model(&User{}).Count(&count).Error; err == nil {
			response.Database = "connected"
			response.UserCount = &count
		} else {
			log.Printf("Database health check failed: %v", err)
		}
	}

	c.JSON(http.StatusOK, response)
}

func internalHealthCheckHandler(c *gin.Context) {
	response := InternalCheckResponse{
		Status:    "healthy",
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Get the current server's address (assuming it's running on the same port)
	baseURL := "http://localhost:8080"
	if port := os.Getenv("PORT"); port != "" {
		baseURL = "http://localhost:" + port
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), "GET", baseURL+"/health", nil)
	if err != nil {
		response.InternalError = fmt.Sprintf("Failed to create request: %v", err)
		c.JSON(http.StatusInternalServerError, response)
		return
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		response.InternalError = fmt.Sprintf("Failed to make internal request: %v", err)
		c.JSON(http.StatusInternalServerError, response)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		response.InternalError = fmt.Sprintf("Failed to read response body: %v", err)
		c.JSON(http.StatusInternalServerError, response)
		return
	}

	var healthResp HealthResponse
	if err := json.Unmarshal(body, &healthResp); err != nil {
		response.InternalError = fmt.Sprintf("Failed to parse response: %v", err)
		c.JSON(http.StatusInternalServerError, response)
		return
	}

	response.InternalHealth = &healthResp
	c.JSON(http.StatusOK, response)
}

func setupRoutes() *gin.Engine {
	// Set Gin to release mode for production
	gin.SetMode(gin.ReleaseMode)
	
	r := gin.Default()

	// Add OpenTelemetry middleware
	r.Use(otelgin.Middleware(os.Getenv("OTEL_SERVICE_NAME")))

	// Add CORS middleware for better API accessibility
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		
		c.Next()
	})



	// Required endpoints
	r.GET("/health", healthHandler)
	r.GET("/internal-check", internalHealthCheckHandler)

	return r
}

func main() {
	// Initialize OpenTelemetry
	ctx := context.Background()
	otelShutdown, err := setupOTelSDK(ctx)
	if err != nil {
		log.Fatalf("Failed to setup OpenTelemetry: %v", err)
	}
	defer func() {
		if err := otelShutdown(ctx); err != nil {
			log.Printf("Error shutting down OpenTelemetry: %v", err)
		}
	}()

	// initialize tracer
	// tracer = otel.Tracer(os.Getenv("OTEL_SERVICE_NAME"))



	// Initialize HTTP client
	initHTTPClient()

	// Initialize database
	initDatabase()

	// Setup routes
	r := setupRoutes()

	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	log.Printf("Health endpoint: http://localhost:%s/health", port)
	log.Printf("Internal check endpoint: http://localhost:%s/internal-check", port)
	
	// Start server
	if err := r.Run(":" + port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
} 