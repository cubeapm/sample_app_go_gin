package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/gin-gonic/gin"
	"github.com/newrelic/go-agent/v3/integrations/nrgin"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

const kafkaTopicName = "sample_topic"

var (
	hcl http.Client
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
	hcl = http.Client{}

	// initialize redis
	rdb = redis.NewClient(&redis.Options{
		Addr: "redis:6379",
	})

	// initialize mongo
	mdbOpts := options.Client().ApplyURI("mongodb://mongo:27017")
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

	// initialize newrelic
	app, err := newrelic.NewApplication(
		newrelic.ConfigFromEnvironment(),
	)

	// Create Gin router
	router := gin.Default()
	router.Use(nrgin.Middleware(app))

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

// todo: All api's call is visible on APM, and the rest of the data and traces — like Redis, MongoDB, external API calls, ClickHouse, and Kafka — is not visible."

func indexFunc(c *gin.Context) {
	c.String(http.StatusOK, "index called")
}

func paramFunc(c *gin.Context) {
	param := c.Param("param")
	c.String(http.StatusOK, "Got param: %s", param)
}

func exceptionFunc(c *gin.Context) {
	c.Status(http.StatusInternalServerError)
}

func apiFunc(c *gin.Context) {
	txn := nrgin.Transaction(c)
	req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, "http://localhost:8000/", nil)
	seg := newrelic.StartExternalSegment(txn, req)
	resp, err := hcl.Do(req)
	if err != nil {
		c.String(http.StatusInternalServerError, "API call error: %v", err)
		return
	}
	defer resp.Body.Close()
	seg.End()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.String(http.StatusInternalServerError, "Read error: %v", err)
		return
	}
	c.String(http.StatusOK, "Got api: %s", respBody)
}

func redisFunc(c *gin.Context) {
	txn := nrgin.Transaction(c)
	seg := newrelic.DatastoreSegment{
		StartTime:  txn.StartSegmentNow(),
		Product:    newrelic.DatastoreRedis,
		Collection: "cache",
		Operation:  "GET",
	}
	val, err := rdb.Get(c.Request.Context(), "key").Result()
	seg.End()

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
	txn := nrgin.Transaction(c)

	seg := newrelic.DatastoreSegment{
		StartTime:  txn.StartSegmentNow(),
		Product:    newrelic.DatastoreMongoDB,
		Collection: "sampleCollection",
		Operation:  "FindOne",
	}
	collection := mdb.Database("sample_db").Collection("sampleCollection")
	_ = collection.FindOne(c.Request.Context(), bson.D{{Key: "name", Value: "dummy"}})
	seg.End()
	c.String(http.StatusOK, "Mongo called")
}

func clickhouseFunc(c *gin.Context) {
	txn := nrgin.Transaction(c)

	seg := newrelic.DatastoreSegment{
		StartTime:  txn.StartSegmentNow(),
		Product:    "ClickHouse", // custom
		Collection: "system",
		Operation:  "Query",
	}
	res, err := ccn.Query(c.Request.Context(), "SELECT NOW()")
	seg.End()

	if err != nil {
		c.String(http.StatusInternalServerError, "Clickhouse query error: %v", err)
		return
	}
	c.String(http.StatusOK, "Clickhouse called: %v", res.Columns())
}

func kafkaProduceFunc(c *gin.Context) {
	txn := nrgin.Transaction(c)

	seg := newrelic.DatastoreSegment{
		StartTime:  txn.StartSegmentNow(),
		Product:    "Kafka",
		Collection: kafkaTopicName,
		Operation:  "Produce",
	}
	kcn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := kcn.WriteMessages(
		kafka.Message{Value: []byte("one!")},
		kafka.Message{Value: []byte("two!")},
		kafka.Message{Value: []byte("three!")},
	)
	seg.End()

	if err != nil {
		c.String(http.StatusInternalServerError, "Kafka produce error: %v", err)
		return
	}
	c.String(http.StatusOK, "Kafka produced")
}

func kafkaConsumeFunc(c *gin.Context) {
	txn := nrgin.Transaction(c)

	seg := newrelic.DatastoreSegment{
		StartTime:  txn.StartSegmentNow(),
		Product:    "Kafka",
		Collection: kafkaTopicName,
		Operation:  "Consume",
	}
	kcn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_ = kcn.ReadBatch(10e3, 1e6) // fetch 10KB min, 1MB max
	seg.End()

	c.String(http.StatusOK, "Kafka consumed")
}
