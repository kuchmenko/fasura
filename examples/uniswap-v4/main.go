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
	configPath := flag.String("config", "examples/uniswap-v4/fasura.yaml", "path to Fasura YAML")
	flag.Parse()

	config, err := configyaml.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	app, err := fasura.New(config)
	if err != nil {
		log.Fatal(err)
	}
	if err := app.Handle("uniswap-v4.Swap", printSwap); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func printSwap(_ context.Context, event fasura.Event) error {
	fmt.Printf("pool=%v sender=%v amount0=%v amount1=%v tx=%s\n", event.Values["id"], event.Values["sender"], event.Values["amount0"], event.Values["amount1"], event.Log.TxHash)
	return nil
}
