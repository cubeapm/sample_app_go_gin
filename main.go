package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"go.elastic.co/apm/module/apmgin/v2"
	"go.elastic.co/apm/module/apmhttp/v2"
	"go.elastic.co/apm/module/apmmongo/v2"
	"go.elastic.co/apm/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

const kafkaTopicName = "sample_topic"

var (
	hcl = *apmhttp.WrapClient(http.DefaultClient)
	rdb *redis.Client
	mdb *mongo.Client
	ccn driver.Conn
	kcn *kafka.Conn
)

func main() {
	if err := run(); err != nil {
		log.Fatalln(err)
	}
}

func run() error {
	var err error

	// initialize http client
	hcl = *apmhttp.WrapClient(&http.Client{})

	// initialize redis
	rdb = redis.NewClient(&redis.Options{
		Addr: "redis:6379",
	})

	// initialize mongo
	mdbOpts := options.Client().ApplyURI("mongodb://mongo:27017")
	// used Built-in instrumentation modules for mongodb
	mdbOpts.Monitor = apmmongo.CommandMonitor()
	mdb, err = mongo.Connect(context.Background(), mdbOpts)
	if err != nil {
		return err
	}
	if err = mdb.Ping(context.Background(), readpref.Primary()); err != nil {
		return err
	}
	defer func() {
		_ = mdb.Disconnect(context.Background())
	}()

	// initialize clickhouse
	ccn, err = clickhouse.Open(&clickhouse.Options{
		Addr: []string{"clickhouse:9000"},
	})
	if err != nil {
		return err
	}
	if err = ccn.Ping(context.Background()); err != nil {
		return err
	}

	// initialize kafka
	kcn, err = kafka.DialLeader(context.Background(), "tcp", "kafka:9092", kafkaTopicName, 0)
	if err != nil {
		return err
	}

	// Create Gin router
	router := gin.Default()
	router.Use(gin.Recovery())
	router.Use(apmgin.Middleware(router))

	// Define routes
	router.GET("/", indexFunc)
	router.GET("/param/:param", paramFunc)
	router.GET("/exception", exceptionFunc)
	router.GET("/api", apiFunc)
	router.GET("/redis", redisFunc)
	router.GET("/mongo", mongoFunc)
	router.GET("/clickhouse", clickhouseFunc)
	router.GET("/kafka/produce", kafkaProduceFunc)
	router.GET("/kafka/consume", kafkaConsumeFunc)

	// Graceful shutdown
	srv := &http.Server{
		Addr:    ":8000",
		Handler: router,
	}

	// Handle SIGINT (CTRL+C)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	srvErr := make(chan error, 1)
	go func() {
		log.Println("Server started on :8000")
		srvErr <- srv.ListenAndServe()
	}()

	select {
	case err = <-srvErr:
		return err
	case <-ctx.Done():
		stop()
		log.Println("Shutting down server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// Handlers

// Built-in instrumentation modules
// (https://www.elastic.co/docs/reference/apm/agents/go/builtin-modules#builtin-modules-apmmongo)

func indexFunc(c *gin.Context) {
	ctx := c.Request.Context()

	// Start a custom span named "indexFunc"
	span, ctx := apm.StartSpan(ctx, "indexFunc", "handler")
	defer span.End()

	c.String(http.StatusOK, "index called")
}

func paramFunc(c *gin.Context) {
	ctx := c.Request.Context()

	// Start custom span for this logic
	span, ctx := apm.StartSpan(ctx, "paramFunc", "handler")
	defer span.End()

	param := c.Param("param")
	c.String(http.StatusOK, "Got param: %s", param)
}

func exceptionFunc(c *gin.Context) {
	ctx := c.Request.Context()
	err := fmt.Errorf("Exception called")
	apm.CaptureError(ctx, err).Send()
	c.Status(http.StatusInternalServerError)
}

func apiFunc(c *gin.Context) {
	req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, "http://localhost:8000/", nil)
	resp, err := hcl.Do(req)
	if err != nil {
		c.String(http.StatusInternalServerError, "API call error: %v", err)
		return
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.String(http.StatusInternalServerError, "Read error: %v", err)
		return
	}
	c.String(http.StatusOK, "Got api: %s", respBody)
}

func redisFunc(c *gin.Context) {
	ctx := c.Request.Context()
	span, ctx := apm.StartSpan(ctx, "Redis GET", "db.redis.query")
	defer span.End()

	val, err := rdb.Get(c.Request.Context(), "key").Result()
	if err == redis.Nil {
		c.String(http.StatusOK, "Redis called")
		return
	} else if err != nil {
		c.String(http.StatusInternalServerError, "Redis error: %v", err)
		return
	}
	c.String(http.StatusOK, "Redis called: %s", val)
}

func mongoFunc(c *gin.Context) {
	collection := mdb.Database("sample_db").Collection("sampleCollection")
	_ = collection.FindOne(c.Request.Context(), bson.D{{Key: "name", Value: "dummy"}})
	c.String(http.StatusOK, "Mongo called")
}

func clickhouseFunc(c *gin.Context) {
	ctx := c.Request.Context()
	span, ctx := apm.StartSpan(ctx, "ClickHouse Query", "db.clickhouse.query")
	defer span.End()

	res, err := ccn.Query(c.Request.Context(), "SELECT NOW()")
	if err != nil {
		c.String(http.StatusInternalServerError, "Clickhouse query error: %v", err)
		return
	}
	c.String(http.StatusOK, "Clickhouse called: %v", res.Columns())
}

func kafkaProduceFunc(c *gin.Context) {
	ctx := c.Request.Context()
	// Create a custom span for the Kafka produce operation
	span, ctx := apm.StartSpan(ctx, "Kafka Produce", "messaging.kafka.produce")
	defer span.End()

	kcn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := kcn.WriteMessages(
		kafka.Message{Value: []byte("one!")},
		kafka.Message{Value: []byte("two!")},
		kafka.Message{Value: []byte("three!")},
	)
	if err != nil {
		c.String(http.StatusInternalServerError, "Kafka produce error: %v", err)
		return
	}
	c.String(http.StatusOK, "Kafka produced")
}

func kafkaConsumeFunc(c *gin.Context) {
	ctx := c.Request.Context()
	// Create a custom span for the Kafka consume operation
	span, ctx := apm.StartSpan(ctx, "Kafka Consume Batch", "messaging.kafka.consume")
	defer span.End()

	kcn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_ = kcn.ReadBatch(10e3, 1e6) // fetch 10KB min, 1MB max
	c.String(http.StatusOK, "Kafka consumed")
}
