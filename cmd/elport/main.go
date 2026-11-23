package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/youthesame/elport/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.RunWithEnv(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, cli.Env{})
	stop()
	os.Exit(code)
}
