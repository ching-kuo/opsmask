package policy

type Policy string

const (
	Pseudonymize Policy = "pseudonymize"
	Destroy      Policy = "destroy"
)

func (p Policy) Valid() bool { return p == Pseudonymize || p == Destroy }

func BuiltinSecretTypes() map[string]bool {
	return map[string]bool{
		"jwt": true, "pem_private_key": true, "password_url": true,
		"aws_key": true, "aws_secret": true, "github_token": true,
		"slack_token": true, "openai_key": true, "anthropic_key": true,
		"gcp_sa": true, "gitlab_token": true, "stripe_key": true,
		"stripe_publishable_key": true, "stripe_webhook_secret": true,
		"gcp_api_key": true, "twilio_key": true,
		"npm_token": true, "pypi_token": true, "sendgrid_key": true,
		"digitalocean_token": true, "linear_token": true, "postman_key": true,
	}
}
