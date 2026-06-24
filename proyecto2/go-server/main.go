package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go-server/proto"
	"google.golang.org/grpc"
)

type server struct {
	proto.UnimplementedMatchPredictionServiceServer
	rabbitCh *amqp.Channel
}

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

	// Reintentar conexión hasta que RabbitMQ esté listo
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

	// Declarar la cola
	_, err = ch.QueueDeclare(
		"predictions", // nombre
		true,          // durable
		false,         // auto-delete
		false,         // exclusive
		false,         // no-wait
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to declare queue: %v", err)
	}

	log.Println("Connected to RabbitMQ successfully")
	return conn, ch
}

func (s *server) SendPrediction(ctx context.Context, req *proto.MatchPredictionRequest) (*proto.MatchPredictionResponse, error) {
	msg := PredictionMessage{
		HomeTeam:  req.HomeTeam.String(),
		AwayTeam:  req.AwayTeam.String(),
		HomeGoals: req.HomeGoals,
		AwayGoals: req.AwayGoals,
		Username:  req.Username,
		Timestamp: req.Timestamp,
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return &proto.MatchPredictionResponse{Status: "error"}, err
	}

	err = s.rabbitCh.Publish(
		"",            // exchange
		"predictions", // routing key (cola)
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
	if err != nil {
		return &proto.MatchPredictionResponse{Status: "error"}, err
	}

	log.Printf("Published prediction: %s vs %s by %s", msg.HomeTeam, msg.AwayTeam, msg.Username)
	return &proto.MatchPredictionResponse{Status: "ok"}, nil
}

func main() {
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@rabbitmq:5672/"
	}

	_, rabbitCh := connectRabbitMQ(rabbitURL)

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	proto.RegisterMatchPredictionServiceServer(grpcServer, &server{
		rabbitCh: rabbitCh,
	})

	log.Println("gRPC Server running on port 50051...")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
