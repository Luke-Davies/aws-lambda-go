# AWS Lambda for Go 

An experiment reducing the go lambda runtime to a single function signature

# Getting Started

``` Go
// main.go
package main

import (
	"context"

	"github.com/Luke-Davies/aws-lambda-go/lambda"
)

type Request struct {
	Message string
}

type Response struct {
	Message string
}

func hello(ctx context.Context, event Request) (Response, error) {
	return Response{ Message: "Hello λ!" }, nil
}

func main() {
	// Make the handler available for Remote Procedure Call by AWS Lambda
	lambda.StartHandlerFunc(hello)
}
```
