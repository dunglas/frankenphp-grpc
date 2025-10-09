package grpc

import (
	"github.com/dunglas/frankenphp"
)

var w = &worker{
	Worker: frankenphp.NewWorker("m#Grpc", "", 0, nil),
}

type worker struct {
	frankenphp.Worker

	minThread int
	filename  string
}

func (w *worker) FileName() string {
	return w.filename
}

func (w *worker) GetMinThreads() int {
	return w.minThread
}
