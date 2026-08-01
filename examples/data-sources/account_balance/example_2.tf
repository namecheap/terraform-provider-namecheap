data "namecheap_account_balance" "current" {}

data "namecheap_tld_pricing" "com" {
  tld    = "com"
  action = "REGISTER"
}

output "planned_registration" {
  value = data.namecheap_tld_pricing.com.price

  precondition {
    condition     = tonumber(data.namecheap_account_balance.current.available_balance) >= tonumber(data.namecheap_tld_pricing.com.price)
    error_message = "Insufficient Namecheap balance for registration."
  }
}
