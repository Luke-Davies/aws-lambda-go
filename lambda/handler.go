// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.

package lambda

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil" // nolint:staticcheck

	"github.com/aws/aws-lambda-go/lambda/handlertrace"
)

type Handler interface {
	Invoke(ctx context.Context, payload []byte) ([]byte, error)
}

type handlerOptions struct {
	handlerFunc
	baseContext              context.Context
	jsonResponseEscapeHTML   bool
	jsonResponseIndentPrefix string
	jsonResponseIndentValue  string
	enableSIGTERM            bool
	sigtermCallbacks         []func()
}

type Option func(*handlerOptions)

// WithContext is a HandlerOption that sets the base context for all invocations of the handler.
//
// Usage:
//
//	lambda.StartWithOptions(
//	 	func (ctx context.Context) (string, error) {
//	 		return ctx.Value("foo"), nil
//	 	},
//	 	lambda.WithContext(context.WithValue(context.Background(), "foo", "bar"))
//	)
func WithContext(ctx context.Context) Option {
	return Option(func(h *handlerOptions) {
		h.baseContext = ctx
	})
}

// WithSetEscapeHTML sets the SetEscapeHTML argument on the underlying json encoder
//
// Usage:
//
//	lambda.StartWithOptions(
//		func () (string, error) {
//			return "<html><body>hello!></body></html>", nil
//		},
//		lambda.WithSetEscapeHTML(true),
//	)
func WithSetEscapeHTML(escapeHTML bool) Option {
	return Option(func(h *handlerOptions) {
		h.jsonResponseEscapeHTML = escapeHTML
	})
}

// WithSetIndent sets the SetIndent argument on the underling json encoder
//
// Usage:
//
//	lambda.StartWithOptions(
//		func (event any) (any, error) {
//			return event, nil
//		},
//		lambda.WithSetIndent(">"," "),
//	)
func WithSetIndent(prefix, indent string) Option {
	return Option(func(h *handlerOptions) {
		h.jsonResponseIndentPrefix = prefix
		h.jsonResponseIndentValue = indent
	})
}

// WithEnableSIGTERM enables SIGTERM behavior within the Lambda platform on container spindown.
// SIGKILL will occur ~500ms after SIGTERM.
// Optionally, an array of callback functions to run on SIGTERM may be provided.
//
// Usage:
//
//	lambda.StartWithOptions(
//	    func (event any) (any, error) {
//			return event, nil
//		},
//		lambda.WithEnableSIGTERM(func() {
//			log.Print("function container shutting down...")
//		})
//	)
func WithEnableSIGTERM(callbacks ...func()) Option {
	return Option(func(h *handlerOptions) {
		h.sigtermCallbacks = append(h.sigtermCallbacks, callbacks...)
		h.enableSIGTERM = true
	})
}

func newHandler[TIn any, TOut any, H HandlerFunc[TIn, TOut]](handlerFunc H, options ...Option) *handlerOptions {
	h := &handlerOptions{
		baseContext:              context.Background(),
		jsonResponseEscapeHTML:   false,
		jsonResponseIndentPrefix: "",
		jsonResponseIndentValue:  "",
	}
	for _, option := range options {
		option(h)
	}
	if h.enableSIGTERM {
		enableSIGTERM(h.sigtermCallbacks)
	}
	h.handlerFunc = reflectHandler(handlerFunc, h)
	return h
}

type handlerFunc func(context.Context, []byte) (io.Reader, error)

// back-compat for the rpc mode
func (h handlerFunc) Invoke(ctx context.Context, payload []byte) ([]byte, error) {
	response, err := h(ctx, payload)
	if err != nil {
		return nil, err
	}
	// if the response needs to be closed (ex: net.Conn, os.File), ensure it's closed before the next invoke to prevent a resource leak
	if response, ok := response.(io.Closer); ok {
		defer response.Close()
	}
	// optimization: if the response is a *bytes.Buffer, a copy can be eliminated
	switch response := response.(type) {
	case *jsonOutBuffer:
		return response.Bytes(), nil
	case *bytes.Buffer:
		return response.Bytes(), nil
	}
	b, err := ioutil.ReadAll(response)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func errorHandler(err error) handlerFunc {
	return func(_ context.Context, _ []byte) (io.Reader, error) {
		return nil, err
	}
}

type jsonOutBuffer struct {
	*bytes.Buffer
}

func (j *jsonOutBuffer) ContentType() string {
	return contentTypeJSON
}

func reflectHandler[TIn any, TOut any, H HandlerFunc[TIn, TOut]](f H, h *handlerOptions) handlerFunc {
	if f == nil {
		return errorHandler(errors.New("handler is nil"))
	}

	out := &jsonOutBuffer{bytes.NewBuffer(nil)}
	return func(ctx context.Context, payload []byte) (io.Reader, error) {
		out.Reset()
		in := bytes.NewBuffer(payload)
		decoder := json.NewDecoder(in)
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(h.jsonResponseEscapeHTML)
		encoder.SetIndent(h.jsonResponseIndentPrefix, h.jsonResponseIndentValue)

		trace := handlertrace.FromContext(ctx)

		event := new(TIn)
		if err := decoder.Decode(event); err != nil {
			return nil, err
		}
		if nil != trace.RequestEvent {
			trace.RequestEvent(ctx, event)
		}

		response, err := f(ctx, *event)
		if err != nil {
			return nil, err
		}

		if nil != trace.ResponseEvent {
			trace.ResponseEvent(ctx, response)
		}

		// encode to JSON
		if err := encoder.Encode(response); err != nil {
			return nil, err
		}

		// back-compat, strip the encoder's trailing newline unless WithSetIndent was used
		if h.jsonResponseIndentValue == "" && h.jsonResponseIndentPrefix == "" {
			out.Truncate(out.Len() - 1)
		}
		return out, nil
	}
}
