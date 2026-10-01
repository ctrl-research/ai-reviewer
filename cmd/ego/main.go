// Command ego reviews a pull request with an LLM and posts the result
// as a sticky PR comment. It is the engine behind the ego GitHub
// Action and can also be run locally:
//
//	EGO_TOKEN=... EGO_API_KEY=... ego --repo owner/name --pr 42 --post-comment=false
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/ctrl-research/ego/internal/forge"
	"github.com/ctrl-research/ego/internal/gha"
	"github.com/ctrl-research/ego/internal/httpx"
	"github.com/ctrl-research/ego/internal/llm"
	"github.com/ctrl-research/ego/internal/review"
)

// version is set at build time with -ldflags "-X main.version=X.Y.Z".
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	log := gha.NewLogger()

	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version)
		return 0
	}
	cfg, err := review.Load(os.Getenv, os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		log.Errorf("%v", err)
		return 2
	}
	s, err := cfg.Validate()
	if err != nil {
		log.Errorf("%v", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	reviewer, err := llm.New(s.Provider, s.BaseURL, s.APIKey)
	if err != nil {
		log.Errorf("%v", err)
		return 1
	}
	f := forge.NewClient(s.Platform, s.APIURL, s.Repo, s.Token)

	text, err := review.Run(ctx, s, f, reviewer, log)
	if err != nil {
		log.Errorf("%v", err)
		var serr *httpx.StatusError
		if errors.As(err, &serr) && serr.Body != "" {
			log.Infof("%s", serr.Body)
		}
		return 1
	}

	if out := os.Getenv("GITHUB_OUTPUT"); out != "" {
		if err := gha.SetOutput(out, "review", text); err != nil {
			log.Errorf("%v", err)
			return 1
		}
	} else {
		fmt.Println(text)
	}
	return 0
}
