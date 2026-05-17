package netconf

import (
	"fmt"

	"github.com/akam1o/arca-router/pkg/config"
)

func validateConfigSemantics(rpcName string, cfg *config.Config) *RPCError {
	if err := ValidateConfig(cfg); err != nil {
		if rpcErr, ok := err.(*RPCError); ok {
			return rpcErr.WithPath(configValidationErrorPath(rpcName))
		}
		return ErrConfigValidationFailed(rpcName, fmt.Sprintf("validation error: %v", err))
	}
	if err := cfg.Validate(); err != nil {
		return ErrConfigValidationFailed(rpcName, fmt.Sprintf("validation error: %v", err))
	}
	return nil
}

func configValidationErrorPath(rpcName string) string {
	switch rpcName {
	case "edit-config":
		return "/rpc/edit-config/config"
	case "copy-config":
		return "/rpc/copy-config/source"
	case "validate":
		return "/rpc/validate/source"
	default:
		return "/rpc/" + rpcName
	}
}
