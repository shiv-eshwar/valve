package main

import (
	"context"

	"github.com/shiv-eshwar/valve/pkg/api"
	valvepb "github.com/shiv-eshwar/valve/pkg/gen/valve/v1"
	"github.com/shiv-eshwar/valve/pkg/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type grpcServer struct {
	valvepb.UnimplementedRateLimitServer
	lim api.Limiter
	log *logx.Logger
}

func (s *grpcServer) Check(ctx context.Context, req *valvepb.CheckRequest) (*valvepb.CheckResponse, error) {
	key := keyFromPB(req.GetKey())
	d, err := s.lim.Check(ctx, key, limitsFromPB(req.GetLimits()), costFromPB(req.GetCost()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !d.Allowed {
		s.log.Deny(key, d)
	}
	return &valvepb.CheckResponse{Decision: decisionToPB(d)}, nil
}

func (s *grpcServer) Settle(ctx context.Context, req *valvepb.SettleRequest) (*valvepb.SettleResponse, error) {
	var (
		d   api.Decision
		err error
	)
	if req.ActualInputTokens != nil || req.ActualOutputTokens != nil {
		d, err = s.lim.SettleIO(ctx, req.GetReservationId(), req.GetActualInputTokens(), req.GetActualOutputTokens())
	} else {
		d, err = s.lim.Settle(ctx, req.GetReservationId(), req.GetActualTokens())
	}
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return &valvepb.SettleResponse{Decision: decisionToPB(d)}, nil
}

func (s *grpcServer) Refund(ctx context.Context, req *valvepb.RefundRequest) (*valvepb.RefundResponse, error) {
	if err := s.lim.Refund(ctx, req.GetReservationId()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return &valvepb.RefundResponse{}, nil
}
