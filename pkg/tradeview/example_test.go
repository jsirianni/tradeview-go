package tradeview_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jsirianni/tradeview-go/pkg/tradeview"
)

func ExampleWithLogger() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	client, err := tradeview.NewClientWithResponses("https://broker.example/api",
		tradeview.WithLogger(logger),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	_ = client // Use the configured client for finite calls or streams.
}

func ExampleClientWithResponses_GetAccountsWithResponse() {
	client, err := tradeview.NewClientWithResponses(
		"https://broker.example/api",
		tradeview.WithBearerToken("caller-supplied-token"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	response, err := client.GetAccountsWithResponse(ctx, &tradeview.GetAccountsParams{
		Locale: tradeview.Locale("en"),
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(response.StatusCode())
}

func ExampleClient_OpenStreamOrders() {
	client, err := tradeview.NewClient(
		"https://broker.example/api",
		tradeview.WithBearerToken("caller-supplied-token"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.OpenStreamOrders(ctx, "account-id", &tradeview.StreamOrdersParams{
		Locale: tradeview.Locale("en"),
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer stream.Close()
	for stream.Next() {
		event := stream.Value()
		fmt.Println(event.Kind)
	}
	if err := stream.Err(); err != nil {
		fmt.Println(err)
	}
}
