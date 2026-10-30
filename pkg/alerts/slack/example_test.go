package slack_test

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jsirianni/tradeview-go/pkg/alerts"
	"github.com/jsirianni/tradeview-go/pkg/alerts/slack"
	"github.com/jsirianni/tradeview-go/pkg/tradeview"
)

func ExampleClient_Send() {
	broker, err := tradeview.NewClientWithResponses("https://broker.example/api",
		tradeview.WithBearerToken(os.Getenv("BROKER_TOKEN")),
	)
	if err != nil {
		log.Print(err)
		return
	}
	sender, err := slack.NewClient(os.Getenv("SLACK_BOT_TOKEN"))
	if err != nil {
		log.Print(err)
		return
	}

	// The application's dependencies keep broker access and delivery separate.
	app := struct {
		Broker *tradeview.ClientWithResponses
		Alerts alerts.Sender
	}{Broker: broker, Alerts: sender}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	response, err := app.Broker.GetAccountsWithResponse(ctx, &tradeview.GetAccountsParams{
		Locale: tradeview.Locale("en"),
	})
	if err != nil {
		log.Print(err)
		return
	}
	if response.StatusCode() != http.StatusOK {
		log.Printf("unexpected broker status: %d", response.StatusCode())
		return
	}

	// This application chooses to notify a user when an account refresh succeeds.
	// A trading application can evaluate its own rules before calling Send.
	if err := app.Alerts.Send(ctx, "U1234567890", alerts.Alert{
		Title: "Account refresh completed",
		Body:  "New broker account data is available.",
	}); err != nil {
		log.Print(err)
	}
}
