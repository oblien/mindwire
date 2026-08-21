package mindwire_test

import (
	"context"
	"errors"
	"fmt"
	"log"

	mindwire "github.com/oblien/mindwire/daemon/sdk"
)

// These examples compile against the real public surface (the external mindwire_test package imports
// the SDK the way a consumer would) but carry no // Output: line, so `go test` type-checks them without
// executing — they need a configured agent and would otherwise start a real turn.

// Construct a client, run a turn, stream its events, and wait for the result.
func Example() {
	client, err := mindwire.New(mindwire.Options{Agent: "claude-code", CWD: "/path/to/project"})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	ctx := context.Background()
	run, err := client.Turn(ctx, mindwire.TurnRequest{ChatID: "chat-1", Message: "List the files here."})
	if err != nil {
		log.Fatal(err)
	}

	for ev := range run.Stream(ctx) {
		if ev.Type == mindwire.EventText {
			fmt.Print(ev.Text)
		}
	}

	res, err := run.Wait(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Run.Status)
}

// Turn options are addressed with agent-agnostic canonical keys; the runner maps each to the selected
// agent's own field. A capability the agent lacks surfaces as an *APIError with Status 400.
func ExampleClient_Turn() {
	client, err := mindwire.New(mindwire.Options{Agent: "claude-code"})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	_, err = client.Turn(context.Background(), mindwire.TurnRequest{
		ChatID:  "chat-1",
		Message: "Refactor this package.",
		Options: mindwire.TurnOptions{Settings: map[string]string{mindwire.CanonModel: "opus"}},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// Wait returns a *RunFailedError when a turn ends in a non-"done" terminal state; branch on it with
// errors.As, or pass NoErrorOnFailure to inspect WaitResult.Run.Status yourself.
func ExampleRun_Wait() {
	client, err := mindwire.New(mindwire.Options{Agent: "claude-code"})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	run, err := client.Turn(context.Background(), mindwire.TurnRequest{ChatID: "chat-1", Message: "hi"})
	if err != nil {
		log.Fatal(err)
	}
	res, err := run.Wait(context.Background())
	var failed *mindwire.RunFailedError
	switch {
	case err == nil:
		fmt.Println("done:", res.Result.Text)
	case errors.As(err, &failed):
		fmt.Println("turn failed:", failed.Detail)
	default:
		log.Fatal(err)
	}
}
