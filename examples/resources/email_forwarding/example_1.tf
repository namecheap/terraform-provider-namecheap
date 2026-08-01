resource "namecheap_domain_records" "example-com" {
  domain     = "example.com"
  mode       = "OVERWRITE"
  email_type = "FWD"

  record {
    hostname = "@"
    type     = "A"
    address  = "203.0.113.10"
  }
}

resource "namecheap_email_forwarding" "example-com" {
  domain = "example.com"

  forwards = {
    info  = "me@example.com"
    sales = "sales-team@example.com"
    # "*" is the catch-all alias
    "*" = "catchall@example.com"
  }

  depends_on = [namecheap_domain_records.example-com]
}
