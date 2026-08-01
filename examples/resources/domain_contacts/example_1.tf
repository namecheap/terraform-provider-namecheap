resource "namecheap_domain_contacts" "main" {
  domain = "example.com"

  registrant {
    first_name     = "Jane"
    last_name      = "Doe"
    organization   = "Example Corp"
    address1       = "1 Main St"
    city           = "Lisbon"
    state_province = "Lisboa"
    postal_code    = "1000-001"
    country        = "PT"
    phone          = "+351.123456789"
    email_address  = "hostmaster@example.com"
  }

  # tech, admin and aux_billing are optional. When omitted they default to the
  # registrant values; the defaulting is shown in the plan (it is not hidden
  # server-side). Provide a block to override a specific contact:
  #
  # tech {
  #   first_name     = "Tim"
  #   ...
  # }
}
