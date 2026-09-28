//go:build !windows

package doctor

import "github.com/d3v0psdan/bench/daemon/internal/api"

func checkVCRuntime() api.Check { return api.Check{} } // Windows only
