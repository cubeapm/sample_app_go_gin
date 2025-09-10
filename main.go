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
	gintrace "github.com/DataDog/dd-trace-go/contrib/gin-gonic/gin/v2"
	mongotrace "github.com/DataDog/dd-trace-go/contrib/go.mongodb.org/mongo-driver.v2/v2/mongo"
	ddhttp "github.com/DataDog/dd-trace-go/contrib/net/http/v2"
	kafkatrace "github.com/DataDog/dd-trace-go/contrib/segmentio/kafka-go/v2"
	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	redistrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/redis/go-redis.v9"
)

const kafkaTopicName = "sample_topic"

var (
	hcl http.Client
	rdb redis.UniversalClient
	mdb *mongo.Client
	ccn driver.Conn
	kcn *kafka.Conn
	kw  *kafkatrace.KafkaWriter
	kr  *kafkatrace.Reader
)

func main() {
	tracer.Start()
	defer tracer.Stop()
	if err := run(); err != nil {
		log.Fatalln(err)
	}
}

func run() error {
	var err error

	// initialize http client
	// wrap your existing http client for external api calls (Datadog provides a wrapper for the {ddhttp.WrapClient()} http.Client that will automatically generate spans for all HTTP calls,)
	hcl = *ddhttp.WrapClient(&http.Client{})

	// initialize redis
	rdb = redistrace.NewClient(&redis.Options{
		Addr: "redis:6379",
	})

	// initialize mongo
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mdbOpts := options.Client().ApplyURI("mongodb://mongo:27017")
	mdbOpts.Monitor = mongotrace.NewMonitor()
	mdb, err = mongo.Connect(mdbOpts)
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
	// Producer
	kw = kafkatrace.NewWriter(kafka.WriterConfig{
		Brokers: []string{"kafka:9092"},
		Topic:   kafkaTopicName,
	})

	// Consumer
	kr = kafkatrace.NewReader(kafka.ReaderConfig{
		Brokers: []string{"kafka:9092"},
		Topic:   kafkaTopicName,
		GroupID: "my-group",
	})
	defer func() {
		if kw != nil {
			_ = kw.Close()
		}
		if kr != nil {
			_ = kr.Close()
		}
	}()

	// Create Gin router
	router := gin.Default()

	router.Use(gintrace.Middleware("my-service"))

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

// these external apis & databases like Mongo, Redis, Clickhouse, Kafka does not identify as a database in CubeAPM. Although ,we can create custom spans for these databases.
// https://docs.datadoghq.com/tracing/trace_collection/custom_instrumentation/go/dd-api/
// Library compatibility - https://docs.datadoghq.com/tracing/trace_collection/compatibility/go/?tab=v2

func indexFunc(c *gin.Context) {
	c.String(http.StatusOK, "index called")
}

func paramFunc(c *gin.Context) {
	param := c.Param("param")
	span, _ := tracer.StartSpanFromContext(c.Request.Context(), "manual.param.span")
	span.SetTag("param", param)
	span.Finish()

	c.String(http.StatusOK, "Got param: %s", param)
}

func exceptionFunc(c *gin.Context) {
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
	val, err := rdb.Get(c.Request.Context(), "key").Result()
	if err != nil {
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
	span, ctx := tracer.StartSpanFromContext(
		c.Request.Context(),
		"clickhouse.query",
		tracer.ResourceName("SELECT NOW()"),
		tracer.Tag("component", "clickhouse"),
		tracer.Tag("db.system", "clickhouse"),
	)
	defer span.Finish()
	res, err := ccn.Query(ctx, "SELECT NOW()")
	if err != nil {
		c.String(http.StatusInternalServerError, "Clickhouse query error: %v", err)
		return
	}
	span.SetTag("span.kind", "client")
	span.SetTag("db.statement", "SELECT NOW()")
	span.SetTag("db.rows", len(res.Columns()))

	c.String(http.StatusOK, "Clickhouse called: %v", res.Columns())
}

func kafkaProduceFunc(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	err := kw.WriteMessages(ctx,
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
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	msg, err := kr.ReadMessage(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, "Kafka consume error: %v", err)
		return
	}

	c.String(http.StatusOK, "Kafka consumed: %s", string(msg.Value))
}
