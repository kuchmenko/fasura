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
	configPath := flag.String("config", "examples/ctf/fasura.yaml", "path to Fasura YAML")
	flag.Parse()

	config, err := configyaml.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	app, err := fasura.New(config)
	if err != nil {
		log.Fatal(err)
	}
	if err := app.Handle("polymarket.OrderFilled", handleOrderFilled); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func handleOrderFilled(_ context.Context, event fasura.Event) error {
	fmt.Printf("maker=%v taker=%v token=%v makerAmount=%v takerAmount=%v tx=%s\n", event.Values["maker"], event.Values["taker"], event.Values["tokenId"], event.Values["makerAmountFilled"], event.Values["takerAmountFilled"], event.Log.TxHash)
	return nil
}
