## 1.4.0 (Unreleased)

ADDITIONS

- Support `terraform import` for `namecheap_record`
   - Example: `terraform import namecheap_record.foo 'foo.domain.tld/A/127.0.0.1'`

## 1.3.0 (May 22nd 2019)

UPGRADES

- Upgraded Terraform to `v0.12.0`
   - Please [read the release notes](https://github.com/hashicorp/terraform/releases/tag/v0.12.0) and [Terraform upgrade guide](https://www.terraform.io/upgrade-guides/0-12.html)
- Updated various dependencies and the adamdecaf/namecheap library

IMPROVEMENTS

- namecheap: lower max retry attemts and time.Sleep period

BUILD

- build: remove vendor/ directory

## 1.2.0 (Feb 26th 2019)

IMPROVEMENTS

- Added backoff and retry for namecheap API errors.

## 1.1.1 (December 4th 2018)

BUG FIXES

- Fixed timeout on record create, delete, and updates. ([#7](https://github.com/adamdecaf/terraform-provider-namecheap/issues/7))

## 1.1.0 (June 8th 2018)

ADDITIONS

* Added `namecheap_ns` record ([#3](https://github.com/adamdecaf/terraform-provider-namecheap/pull/3))

## 1.0.0 (September 26, 2017)

* No changes from 0.1.1; just adjusting to [the new version numbering scheme](https://www.hashicorp.com/blog/hashicorp-terraform-provider-versioning/).

## 0.1.1 (June 21, 2017)

NOTES:

Bumping the provider version to get around provider caching issues - still same functionality

## 0.1.0 (June 21, 2017)

NOTES:

* Same functionality as that of Terraform 0.9.8. Repacked as part of [Provider Splitout](https://www.hashicorp.com/blog/upcoming-provider-changes-in-terraform-0-10/)
