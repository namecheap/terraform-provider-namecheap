locals {
  managed_domains = ["example.com", "example.net", "example.org"]
}

resource "namecheap_domain_contacts" "fleet" {
  for_each = toset(local.managed_domains)
  domain   = each.value

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
}
