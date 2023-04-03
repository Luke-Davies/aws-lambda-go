// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.

package lambda

import (
	"log"
	"os"
)

type startFunction struct {
	env string
	f   func(envValue string, handler *handlerOptions) error
}

var (
	runtimeAPIStartFunction = &startFunction{
		env: "AWS_LAMBDA_RUNTIME_API",
		f:   startRuntimeAPILoop,
	}
	startFunctions = []*startFunction{runtimeAPIStartFunction}

	// This allows end to end testing of the Start functions, by tests overwriting this function to keep the program alive
	logFatalf = log.Fatalf
)

func start(handler *handlerOptions) {
	var keys []string
	for _, start := range startFunctions {
		config := os.Getenv(start.env)
		if config != "" {
			// in normal operation, the start function never returns
			// if it does, exit!, this triggers a restart of the lambda function
			err := start.f(config, handler)
			logFatalf("%v", err)
		}
		keys = append(keys, start.env)
	}
	logFatalf("expected AWS Lambda environment variables %s are not defined", keys)

}
