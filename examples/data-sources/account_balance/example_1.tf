data "namecheap_account_balance" "current" {}

output "funds" {
  value = "${data.namecheap_account_balance.current.available_balance} ${data.namecheap_account_balance.current.currency}"
}
