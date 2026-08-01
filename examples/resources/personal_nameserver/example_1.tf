resource "namecheap_personal_nameserver" "ns1" {
  domain     = "example.com"
  nameserver = "ns1.example.com"
  ip         = "93.184.216.34"
}

resource "namecheap_personal_nameserver" "ns2" {
  domain     = "example.com"
  nameserver = "ns2.example.com"
  ip         = "93.184.216.35"
}
