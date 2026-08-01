data "namecheap_tld_pricing" "com" {
  tld    = "com"
  action = "REGISTER"
  years  = 1
}

output "com_price" {
  value = "${data.namecheap_tld_pricing.com.price} ${data.namecheap_tld_pricing.com.currency}"
}
