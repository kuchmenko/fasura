package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kuchmenko/fasura"
	"github.com/kuchmenko/fasura/configyaml"
)

func main() {
	configPath := flag.String("config", "examples/erc20/fasura.yaml", "path to Fasura YAML")
	flag.Parse()

	config, err := configyaml.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	app, err := fasura.New(config)
	if err != nil {
		log.Fatal(err)
	}
	if err := app.HandleAll(printEvent); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func printEvent(_ context.Context, event fasura.Event) error {
	fmt.Printf("chain=%s event=%s contract=%s values=%v tx=%s\n", event.Chain, event.Key(), event.Address, event.Values, event.Log.TxHash)
	return nil
}
