# Each record is managed independently; everything else in the zone is left alone.
resource "namecheap_domain_host_record" "www" {
  domain   = "example.com"
  hostname = "www"
  type     = "A"
  address  = "203.0.113.10"
  ttl      = 1800
}

resource "namecheap_domain_host_record" "spf" {
  domain   = "example.com"
  hostname = "@"
  type     = "TXT"
  address  = "v=spf1 include:_spf.example.net -all"
}
