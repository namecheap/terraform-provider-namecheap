# Credentials are read from NAMECHEAP_USER_NAME, NAMECHEAP_API_USER,
# NAMECHEAP_API_KEY and NAMECHEAP_CLIENT_IP when the arguments are omitted, which
# is what you want in CI — see the "CI and automation environments" guide.
provider "namecheap" {
  use_sandbox = false
}
