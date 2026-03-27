package temporal

import (
	"context"

	infraCli "github.com/go-modulus/modulus/cli"
	"github.com/go-modulus/temporal/errors"
	"github.com/urfave/cli/v3"
	"go.temporal.io/sdk/client"
	interceptor2 "go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"
	"go.uber.org/fx"
)

type Worker struct {
	runner      *infraCli.Runner
	temporal    client.Client
	registerers []Registerer
}

type WorkersParams struct {
	fx.In

	Runner      *infraCli.Runner
	Temporal    client.Client
	Registerers []Registerer `group:"temporal.registerers"`
}

func NewWorker(params WorkersParams) *Worker {
	return &Worker{
		runner:      params.Runner,
		temporal:    params.Temporal,
		registerers: params.Registerers,
	}
}

func WorkerCommand(w *Worker) *cli.Command {
	return &cli.Command{
		Name:   "worker",
		Action: w.Invoke,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "queue",
				Aliases:  []string{"q"},
				Usage:    "queue name",
				Required: true,
			},
			&cli.BoolFlag{
				Name:    "enable-session-worker",
				Aliases: []string{"s"},
				Usage:   "enable session worker",
				Value:   false,
			},
		},
	}
}

func (w *Worker) Invoke(ctx context.Context, cmd *cli.Command) error {
	queue := cmd.String("queue")
	enableSessionWorker := cmd.Bool("enable-session-worker")
	return w.runner.Run(
		ctx,
		func(ctx context.Context) error {
			errorInterceptor := &errors.AppErrWrapWorkerInterceptor{}
			tw := worker.New(
				w.temporal, queue, worker.Options{
					EnableSessionWorker: enableSessionWorker,
					Interceptors:        []interceptor2.WorkerInterceptor{errorInterceptor},
				},
			)

			for _, r := range w.registerers {
				r.Register(tw)
			}

			return tw.Run(w.interruptCh(ctx))
		},
	)
}

func (w *Worker) interruptCh(ctx context.Context) <-chan interface{} {
	interruptCh := make(chan interface{}, 1)
	go func() {
		<-ctx.Done()

		interruptCh <- struct{}{}
	}()

	return interruptCh
}
