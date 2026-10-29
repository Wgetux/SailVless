//go:build !linux

package service

import "context"

// WatchNetwork is only implemented on Linux.
func (s *Service) WatchNetwork(ctx context.Context) {}
