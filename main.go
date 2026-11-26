// Package main is the entrypoint of the cert-manager ACME DNS01 solver webhook
// for the Nameshield DNS API (https://docs.nameshield.net/).
package main

import (
	"os"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"

	"github.com/dotinfra/cert-manager-webhook-nameshield/nameshield"
)

// GroupName is the API group name under which the webhook solver is served.
// It must be set via the GROUP_NAME environment variable and must match the
// `groupName` value of the Helm chart and of the Issuer's webhook stanza.
var GroupName = os.Getenv("GROUP_NAME")

func main() {
	if GroupName == "" {
		panic("GROUP_NAME must be specified")
	}

	// This will register our custom DNS provider with the webhook serving
	// library, making it available as an API under the provided GroupName.
	cmd.RunWebhookServer(GroupName,
		nameshield.New(),
	)
}
