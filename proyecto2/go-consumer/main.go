package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
)

type PredictionMessage struct {
	HomeTeam  string `json:"home_team"`
	AwayTeam  string `json:"away_team"`
	HomeGoals int32  `json:"home_goals"`
	AwayGoals int32  `json:"away_goals"`
	Username  string `json:"username"`
	Timestamp string `json:"timestamp"`
}

func connectRabbitMQ(url string) (*amqp.Connection, *amqp.Channel) {
	var conn *amqp.Connection
	var err error

	for i := 0; i < 10; i++ {
		conn, err = amqp.Dial(url)
		if err == nil {
			break
		}
		log.Printf("RabbitMQ not ready, retrying in 5s... (%d/10)", i+1)
		time.Sleep(5 * time.Second)
	}
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open channel: %v", err)
	}

	_, err = ch.QueueDeclare(
		"predictions",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to declare queue: %v", err)
	}

	return conn, ch
}

func connectValkey(url string) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr: url,
	})

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, err := client.Ping(ctx).Result()
		if err == nil {
			log.Println("Connected to Valkey successfully")
			return client
		}
		log.Printf("Valkey not ready, retrying in 5s... (%d/10)", i+1)
		time.Sleep(5 * time.Second)
	}
	log.Fatalf("Failed to connect to Valkey")
	return nil
}

func storePrediction(ctx context.Context, rdb *redis.Client, msg PredictionMessage) {
	// Contador total de predicciones
	rdb.Incr(ctx, "total_predictions")

	// Contador por usuario
	rdb.Incr(ctx, fmt.Sprintf("user:%s:predictions", msg.Username))

	// Goles por equipo local
	rdb.RPush(ctx, fmt.Sprintf("team:%s:home_goals", msg.HomeTeam), msg.HomeGoals)

	// Goles por equipo visitante
	rdb.RPush(ctx, fmt.Sprintf("team:%s:away_goals", msg.AwayTeam), msg.AwayGoals)

	// Determinar ganador y contabilizar victoria
	if msg.HomeGoals > msg.AwayGoals {
		rdb.Incr(ctx, fmt.Sprintf("team:%s:wins", msg.HomeTeam))
	} else if msg.AwayGoals > msg.HomeGoals {
		rdb.Incr(ctx, fmt.Sprintf("team:%s:wins", msg.AwayTeam))
	}

	// Guardar max/min goles local
	rdb.Incr(ctx, fmt.Sprintf("goals:home:%d", msg.HomeGoals))
	rdb.Incr(ctx, fmt.Sprintf("goals:away:%d", msg.AwayGoals))

	// Serie temporal para el equipo asignado (GTM para carnet 201407049)
	assignedTeam := os.Getenv("ASSIGNED_TEAM")
	if assignedTeam == "" {
		assignedTeam = "GTM"
	}
	if msg.HomeTeam == assignedTeam || msg.AwayTeam == assignedTeam {
		rdb.Incr(ctx, fmt.Sprintf("team:%s:total_predictions", assignedTeam))

		// Serie temporal con timestamp
		entry := fmt.Sprintf("%s|%d|%d", msg.Timestamp, msg.HomeGoals, msg.AwayGoals)
		rdb.RPush(ctx, fmt.Sprintf("team:%s:timeseries", assignedTeam), entry)
		// TTL de 7 días para evitar saturación
		rdb.Expire(ctx, fmt.Sprintf("team:%s:timeseries", assignedTeam), 7*24*time.Hour)
	}

	log.Printf("Stored prediction: %s vs %s | %d-%d | user: %s",
		msg.HomeTeam, msg.AwayTeam, msg.HomeGoals, msg.AwayGoals, msg.Username)
}

func main() {
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@rabbitmq:5672/"
	}

	valkeyURL := os.Getenv("VALKEY_URL")
	if valkeyURL == "" {
		valkeyURL = "valkey-service:6379"
	}

	_, rabbitCh := connectRabbitMQ(rabbitURL)
	rdb := connectValkey(valkeyURL)
	ctx := context.Background()

	msgs, err := rabbitCh.Consume(
		"predictions",
		"",
		true,  // auto-ack
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to register consumer: %v", err)
	}

	log.Println("Consumer waiting for messages...")

	for msg := range msgs {
		var prediction PredictionMessage
		if err := json.Unmarshal(msg.Body, &prediction); err != nil {
			log.Printf("Error parsing message: %v", err)
			continue
		}
		storePrediction(ctx, rdb, prediction)
	}
}

