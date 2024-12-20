package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"unicode/utf8"

	"ext_proc_demo/httpcontext"

	"github.com/corazawaf/coraza/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv32 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// ---------------------- Integrated ext_proc Server code ----------------------

type Server struct {
	extprocv3.UnimplementedExternalProcessorServer
	grpc_health_v1.UnimplementedHealthServer

	serverCtx context.Context
	waf       coraza.WAF
	logger    *slog.Logger
}

// NewServer creates a new ext_proc server instance.
func NewServer(serverCtx context.Context, logger *slog.Logger) *Server {
	return &Server{serverCtx: serverCtx, logger: logger}
}

func (s *Server) LoadWAF(waf coraza.WAF) {
	s.waf = waf
}

func (s *Server) Check(context.Context, *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func (s *Server) Watch(*grpc_health_v1.HealthCheckRequest, grpc_health_v1.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "Watch is not implemented")
}

// Process implements ExternalProcessorServer
func (s *Server) Process(stream extprocv3.ExternalProcessor_ProcessServer) (err error) {
	ctx := stream.Context()

	httpCtx := httpcontext.NewHTTPContext(s.waf)
	defer func() {
		if errDone := httpCtx.OnStreamDone(); errDone != nil {
			s.logger.Error("cannot close transaction", slog.String("error", errDone.Error()))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.serverCtx.Done():
			return s.serverCtx.Err()
		default:
		}

		req, err := stream.Recv()
		if errors.Is(err, io.EOF) || status.Code(err) == codes.Canceled {
			return nil
		} else if err != nil {
			s.logger.Error("cannot receive stream request", slog.String("error", err.Error()))
			return status.Errorf(codes.Unknown, "cannot receive stream request: %v", err)
		}

		var interruptionStatus int
		var resp *extprocv3.ProcessingResponse
		switch value := req.Request.(type) {
		case *extprocv3.ProcessingRequest_RequestHeaders:
			setHeaders(httpCtx, req.GetRequestHeaders().Headers, true)
			setAttributes(httpCtx, req.Attributes)
			interruptionStatus, err = httpCtx.OnRequestHeaders()
			if interruptionStatus > 0 {
				resp = newImmediateResponse(interruptionStatus)
			} else {
				resp = headersResponse(httpCtx, true)
			}
		case *extprocv3.ProcessingRequest_RequestBody:
			httpCtx.SetReqBody(value.RequestBody.Body)
			interruptionStatus, err = httpCtx.OnRequestBody()
			httpCtx.SetReqBody(nil)
			if interruptionStatus > 0 {
				resp = newImmediateResponse(interruptionStatus)
			} else {
				resp = defaultRequestBodyResponse
			}
		case *extprocv3.ProcessingRequest_ResponseHeaders:
			setHeaders(httpCtx, req.GetResponseHeaders().Headers, false)
			setAttributes(httpCtx, req.Attributes)
			interruptionStatus, err = httpCtx.OnResponseHeaders(req.GetResponseHeaders().EndOfStream)
			if interruptionStatus > 0 {
				resp = newImmediateResponse(interruptionStatus)
			} else {
				resp = headersResponse(httpCtx, false)
				s.logger.Info("Header mutation applied", slog.String("header", "x-extproc-hello"))
				s.logger.Info("Sending response with headers", slog.Any("headers", resp.Response))
			}
		case *extprocv3.ProcessingRequest_ResponseBody:
			httpCtx.SetResBody(value.ResponseBody.Body)
			interruptionStatus, err = httpCtx.OnResponseBody()
			httpCtx.SetResBody(nil)
			if interruptionStatus > 0 {
				resp = newImmediateResponse(interruptionStatus)
			} else {
				resp = defaultResponseBodyResponse
			}
		default:
			s.logger.Error("unknown request type", slog.String("type", fmt.Sprintf("%T", value)))
			return status.Errorf(codes.InvalidArgument, "unknown request type: %T", value)
		}
		if err != nil {
			s.logger.Error("cannot process request", slog.String("error", err.Error()))
			return status.Errorf(codes.Unknown, "cannot process request: %v", err)
		}
		if err := stream.Send(resp); err != nil {
			s.logger.Error("cannot send response", slog.String("error", err.Error()))
			return status.Errorf(codes.Unknown, "cannot send response: %v", err)
		}
	}
}

func setHeaders(httpCtx *httpcontext.HTTPContext, headers *corev3.HeaderMap, request bool) {
	for _, header := range headers.Headers {
		var value string
		if len(header.RawValue) > 0 {
			if utf8.Valid(header.RawValue) {
				value = string(header.RawValue)
			}
		} else {
			value = header.Value
		}
		if request {
			httpCtx.SetRequestHeader(header.Key, value)
		} else {
			httpCtx.SetResponseHeader(header.Key, value)
		}
	}
}

func setAttributes(httpCtx *httpcontext.HTTPContext, attributes map[string]*structpb.Struct) {
	raw, ok := attributes["envoy.filters.http.ext_proc"]
	if !ok {
		return
	}
	for k, v := range raw.Fields {
		if str := v.GetStringValue(); str != "" {
			httpCtx.SetStringAttribute(k, str)
			continue
		}
		if num := v.GetNumberValue(); num != 0 {
			httpCtx.SetIntAttribute(k, int(num))
			continue
		}
	}
}

// Add custom headers in the ResponseHeaders stage.
func headersResponse(ctx *httpcontext.HTTPContext, request bool) *extprocv3.ProcessingResponse {
	tx := ctx.Transaction()
	if request {
		// If the request body is not accessed.
		if !tx.IsRequestBodyAccessible() {
			return &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_RequestHeaders{},
				ModeOverride: &extprocv32.ProcessingMode{
					RequestBodyMode:    extprocv32.ProcessingMode_NONE,
					ResponseHeaderMode: extprocv32.ProcessingMode_SEND,
					ResponseBodyMode:   extprocv32.ProcessingMode_NONE,
				},
			}
		}
		return defaultRequestHeadersResponse
	} else {
		// Insert a custom header in the response header.
		// No matter what the upstream returns, we insert x-extproc-hello.
		mutation := &extprocv3.HeaderMutation{
			SetHeaders: []*corev3.HeaderValueOption{
				{
					Header: &corev3.HeaderValue{
						Key:      "x-extproc-hello",
						RawValue: []byte("Hello from ext_proc"),
					},
				},
			},
		}

		if !(tx.IsResponseBodyAccessible() && tx.IsResponseBodyProcessable()) {
			// If the response body is not processed.
			return &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_ResponseHeaders{
					ResponseHeaders: &extprocv3.HeadersResponse{
						Response: &extprocv3.CommonResponse{
							HeaderMutation: mutation,
						},
					},
				},
				ModeOverride: &extprocv32.ProcessingMode{
					ResponseBodyMode: extprocv32.ProcessingMode_NONE,
				},
			}
		}
		// It can process the response body by default and also inject headers.
		return &extprocv3.ProcessingResponse{
			Response: &extprocv3.ProcessingResponse_ResponseHeaders{
				ResponseHeaders: &extprocv3.HeadersResponse{
					Response: &extprocv3.CommonResponse{
						HeaderMutation: mutation,
					},
				},
			},
			ModeOverride: &extprocv32.ProcessingMode{
				ResponseBodyMode: extprocv32.ProcessingMode_NONE,
			},
		}
	}
}

var (
	defaultRequestHeadersResponse  = &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestHeaders{}}
	defaultRequestBodyResponse     = &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestBody{}}
	defaultResponseHeadersResponse = &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseHeaders{}}
	defaultResponseBodyResponse    = &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseBody{}}
)

func newImmediateResponse(statusCode int) *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ImmediateResponse{
			ImmediateResponse: &extprocv3.ImmediateResponse{
				Status: &typev3.HttpStatus{Code: typev3.StatusCode(statusCode)},
			},
		},
		ModeOverride: &extprocv32.ProcessingMode{
			ResponseBodyMode:   extprocv32.ProcessingMode_NONE,
			ResponseHeaderMode: extprocv32.ProcessingMode_SKIP,
		},
	}
}

// ---------------------- main function ----------------------

func main() {
	logger := slog.Default()
	grpcServer := grpc.NewServer()

	// Initialize WAF
	waf, err := coraza.NewWAF(coraza.NewWAFConfig())
	if err != nil {
		log.Fatalf("Failed to create WAF: %v", err)
	}

	srv := NewServer(context.Background(), logger)
	srv.LoadWAF(waf) // Load the WAF instance

	extprocv3.RegisterExternalProcessorServer(grpcServer, srv)
	grpc_health_v1.RegisterHealthServer(grpcServer, srv)

	// Enable reflection for debugging
	reflection.Register(grpcServer)

	lis, err := net.Listen("tcp", ":9000")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Println("Starting gRPC ext_proc server on :9000...")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
