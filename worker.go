package temporal

import (
	"context"

	"braces.dev/errtrace"
	infraCli "github.com/go-modulus/modulus/cli"
	apperrors "github.com/go-modulus/temporal/errors"
	"github.com/urfave/cli/v3"
	"go.temporal.io/sdk/client"
	interceptor2 "go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"
	"go.uber.org/fx"
)

type Worker struct {
	runner            *infraCli.Runner
	temporal          client.Client
	registerers       []Registerer
	workerCustomizers []workerCustomizer
}

type WorkersParams struct {
	fx.In

	Runner            *infraCli.Runner
	Temporal          client.Client
	Registerers       []Registerer       `group:"temporal.registerers"`
	WorkerCustomizers []workerCustomizer `group:"temporal.worker_customizers"`
}

type workerCustomizer struct {
	queue     string
	customize func(*worker.Options) error
}

type QueueCustomizer interface {
	Customize(*worker.Options) error
}

// CustomizeQueue configures Temporal worker options for the given task queue.
func CustomizeQueue[T QueueCustomizer](queue string, customizer any) fx.Option {
	if queue == "" {
		return fx.Error(errtrace.New("queue name is required"))
	}
	if customizer == nil {
		return fx.Error(errtrace.New("queue customizer is required"))
	}

	return fx.Provide(
		customizer,
		fx.Annotate(
			func(customize T) workerCustomizer {
				return workerCustomizer{
					queue:     queue,
					customize: customize.Customize,
				}
			},
			fx.ResultTags(`group:"temporal.worker_customizers"`),
		),
	)
}

func NewWorker(params WorkersParams) *Worker {
	return &Worker{
		runner:            params.Runner,
		temporal:          params.Temporal,
		registerers:       params.Registerers,
		workerCustomizers: params.WorkerCustomizers,
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
			errorInterceptor := &apperrors.AppErrWrapWorkerInterceptor{}
			options := worker.Options{
				EnableSessionWorker: enableSessionWorker,
				Interceptors:        []interceptor2.WorkerInterceptor{errorInterceptor},
			}
			for _, customizer := range w.workerCustomizers {
				if customizer.queue != queue {
					continue
				}

				if err := customizer.customize(&options); err != nil {
					return errtrace.Wrap(err)
				}
			}
			tw := worker.New(w.temporal, queue, options)

			for _, r := range w.registerers {
				r.Register(tw)
			}

			return errtrace.Wrap(tw.Run(w.interruptCh(ctx)))
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
