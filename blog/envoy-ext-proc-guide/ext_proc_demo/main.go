package main

import (
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	configPb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

type extProcServer struct {
	extProcPb.UnimplementedExternalProcessorServer
}

// Process handles external processing requests from Envoy.
// It listens for incoming requests, modifies response headers,
// and sends the updated response back to Envoy.
//
// When a request with response headers is received, it adds a custom header
// "x-extproc-hello" with the value "Hello from ext_proc" and returns the modified headers.
//
// Note: The `RawValue` field is used instead of `Value` because it supports
// setting the header value as a byte slice, allowing precise handling of binary data.
//
// This function is called once per HTTP request to process gRPC messages from Envoy.
// It exits when an error occurs while receiving or sending messages.

func (s *extProcServer) Process(
	srv extProcPb.ExternalProcessor_ProcessServer,
) error {
	for {
		req, err := srv.Recv()
		if err != nil {
			return status.Errorf(codes.Unknown, "error receiving request: %v", err)
		}

		log.Printf("Received request: %+v\n", req)

		// Prepare the response to be returned to Envoy.
		resp := &extProcPb.ProcessingResponse{}

		// Only process response headers, not request headers.
		if respHeaders := req.GetResponseHeaders(); respHeaders != nil {
			log.Println("Processing Response Headers...")

			resp = &extProcPb.ProcessingResponse{
				Response: &extProcPb.ProcessingResponse_ResponseHeaders{
					ResponseHeaders: &extProcPb.HeadersResponse{
						Response: &extProcPb.CommonResponse{
							HeaderMutation: &extProcPb.HeaderMutation{
								SetHeaders: []*configPb.HeaderValueOption{
									{
										Header: &configPb.HeaderValue{
											Key:      "x-extproc-hello",
											RawValue: []byte("Hello from ext_proc"),
										},
									},
								},
							},
						},
					},
				},
			}
			log.Printf("Sending response: %+v\n", resp)
			// Send the response back to Envoy.
			if err := srv.Send(resp); err != nil {
				return status.Errorf(codes.Unknown, "error sending response: %v", err)
			}
		} else {
			// If it is not a callback in the response header stage, do not make any modifications and continue processing the next event.
			// For request_headers or other events, do not modify & ensure that Envoy will not be stuck.
			// An empty processing can be returned for request_headers, or it can be skipped in envoy.yaml.
			// Here, simply continue to wait for the next event.
			continue
		}
	}
}

func main() {
	lis, err := net.Listen("tcp", ":9000")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	// Register the ExternalProcessorServer with the gRPC server.
	extProcPb.RegisterExternalProcessorServer(grpcServer, &extProcServer{})

	log.Println("Starting gRPC server on :9000...")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
