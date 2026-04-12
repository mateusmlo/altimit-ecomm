package stripe

import (
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	strp "github.com/stripe/stripe-go/v85"
)

func NewStripeClient(cfg config.StripeConfig) *strp.Client {
	return strp.NewClient(cfg.SecretKey)
}
