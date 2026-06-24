package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"go-client/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type MatchPrediction struct {
	HomeTeam  string `json:"home_team"`
	AwayTeam  string `json:"away_team"`
	HomeGoals int32  `json:"home_goals"`
	AwayGoals int32  `json:"away_goals"`
	Username  string `json:"username"`
	Timestamp string `json:"timestamp"`
}

func teamFromString(team string) proto.Teams {
	switch team {
	case "GTM":
		return proto.Teams_GTM
	case "MEX":
		return proto.Teams_MEX
	case "BRA":
		return proto.Teams_BRA
	case "ARG":
		return proto.Teams_ARG
	case "ESP":
		return proto.Teams_ESP
	default:
		return proto.Teams_TEAMS_UNKNOWN
	}
}

func main() {
	grpcServerURL := os.Getenv("GRPC_SERVER_URL")
	if grpcServerURL == "" {
		grpcServerURL = "go-server:50051"
	}

	// Conectar al gRPC Server
	conn, err := grpc.NewClient(grpcServerURL,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("Failed to connect to gRPC server: %v", err)
	}
	defer conn.Close()

	grpcClient := proto.NewMatchPredictionServiceClient(conn)

	r := gin.Default()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "go-client"})
	})

	r.POST("/predict", func(c *gin.Context) {
		var prediction MatchPrediction
		if err := c.ShouldBindJSON(&prediction); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Enviar via gRPC al servidor
		resp, err := grpcClient.SendPrediction(context.Background(), &proto.MatchPredictionRequest{
			HomeTeam:  teamFromString(prediction.HomeTeam),
			AwayTeam:  teamFromString(prediction.AwayTeam),
			HomeGoals: prediction.HomeGoals,
			AwayGoals: prediction.AwayGoals,
			Username:  prediction.Username,
			Timestamp: prediction.Timestamp,
		})

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": resp.Status})
	})

	log.Println("Go Client running on port 8080...")
	r.Run(":8080")
}
