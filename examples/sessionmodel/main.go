// Sessionmodel selects MiniMax-M3.1-Flash with thinking effort high on one
// session and reads that selection back. It does not send a model turn and it
// does not call SelectModel without a session id.
//
//	MCODE_BIN=mcode MCODE_DATA_DIR=/path/to/isolated-data go run ./examples/sessionmodel
//
// When the executable is node, set MCODE_LEADING to cli.js.
// Leave MCODE_AUTH_MODE empty for an API-key data directory. "desktop" is a
// lease, not the model provider.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/appserver"
	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

const (
	providerID = "minimax_api"
	modelID    = "MiniMax-M3.1-Flash"
	effort     = "high"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	opt := appserver.Options{
		Command:   env("MCODE_BIN", "mcode"),
		DataDir:   os.Getenv("MCODE_DATA_DIR"),
		Workspace: os.Getenv("MCODE_WORKSPACE"),
		AuthMode:  os.Getenv("MCODE_AUTH_MODE"),
	}
	if leading := os.Getenv("MCODE_LEADING"); leading != "" {
		opt.LeadingArgs = strings.Fields(leading)
	}
	client, err := appserver.Start(ctx, opt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()
	desk := client.Desktop()
	before, err := selectedKey(ctx, desk, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "list models: %v\n", err)
		os.Exit(1)
	}
	title := "sdk-session-model"
	created, err := desk.CreateSession(ctx, desktop.CreateSessionReq{Name: "mavis", Title: &title})
	if err != nil || created.SessionID == nil {
		fmt.Fprintf(os.Stderr, "create session: %v\n", err)
		os.Exit(1)
	}
	sessionID, ok := created.SessionID.String()
	if !ok || sessionID == "" {
		fmt.Fprintln(os.Stderr, "session id was empty")
		os.Exit(1)
	}
	thinking := effort
	selected, err := desk.SelectModel(ctx, desktop.SelectModelReq{
		ProviderID: providerID,
		ModelID:    modelID,
		SessionID:  &sessionID,
		Thinking:   &desktop.ModelThinkingInput{Effort: &thinking},
	})
	if err != nil || selected.Success == nil {
		fmt.Fprintf(os.Stderr, "select model: %v\n", err)
		os.Exit(1)
	}
	if flag, ok := selected.Success.Bool(); !ok || !flag {
		fmt.Fprintf(os.Stderr, "select model success was not true\n")
		os.Exit(1)
	}
	got, err := desk.GetSession(ctx, desktop.GetSessionReq{ID: sessionID})
	if err != nil || got.Session == nil || got.Session.Model == nil || got.Session.Model.Thinking == nil || got.Session.Model.Thinking.Effort == nil {
		fmt.Fprintf(os.Stderr, "get session: %v\n", err)
		os.Exit(1)
	}
	gotProvider, _ := got.Session.Model.ProviderID.String()
	gotModel, _ := got.Session.Model.ModelID.String()
	gotEffective, _ := got.Session.EffectiveModel.String()
	if gotProvider != providerID || gotModel != modelID || *got.Session.Model.Thinking.Effort != effort || gotEffective != providerID+"/"+modelID {
		fmt.Fprintf(os.Stderr, "readback provider %q model %q effort %q effective %q\n", gotProvider, gotModel, *got.Session.Model.Thinking.Effort, gotEffective)
		os.Exit(1)
	}
	after, err := selectedKey(ctx, desk, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "list models: %v\n", err)
		os.Exit(1)
	}
	if after != before {
		fmt.Fprintf(os.Stderr, "process selection changed from %q to %q\n", before, after)
		os.Exit(1)
	}
	fmt.Printf("session %s effective_model %s thinking.effort %s\n", sessionID, gotEffective, effort)
}

func selectedKey(ctx context.Context, desk *desktop.Client, sessionID string) (string, error) {
	req := desktop.ListModelsReq{}
	if sessionID != "" {
		req.SessionID = &sessionID
	}
	listed, err := desk.ListModels(ctx, req)
	if err != nil {
		return "", err
	}
	var key string
	for _, item := range listed.Models {
		on, ok := item.Selected.Bool()
		if !ok || !on {
			continue
		}
		provider, _ := item.ProviderID.String()
		model, _ := item.ModelID.String()
		if key != "" {
			return "", fmt.Errorf("more than one selected model")
		}
		key = provider + "/" + model
	}
	return key, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
