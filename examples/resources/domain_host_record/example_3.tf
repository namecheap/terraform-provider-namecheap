# The domain's mail mode, and no records.
resource "namecheap_domain_records" "mail" {
  domain     = "example.com"
  email_type = "GMAIL"
}

# One resource per record this configuration owns. Every other record in the
# zone, such as one a dynamic DNS client keeps up to date, is left alone.
resource "namecheap_domain_host_record" "www" {
  domain   = "example.com"
  hostname = "www"
  type     = "A"
  address  = "203.0.113.10"
}
