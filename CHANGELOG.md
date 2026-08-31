# Changelog

## [2.9.2](https://github.com/namecheap/terraform-provider-namecheap/compare/v2.9.1...v2.9.2) (2026-08-31)


### Bug Fixes

* **deps:** bump go-namecheap-sdk from v2.10.2 to v2.10.3 ([#345](https://github.com/namecheap/terraform-provider-namecheap/issues/345)) ([0bf159f](https://github.com/namecheap/terraform-provider-namecheap/commit/0bf159f7426dd541cbb1a139b5f420bbdc6f3e44))

## [2.9.1](https://github.com/namecheap/terraform-provider-namecheap/compare/v2.9.0...v2.9.1) (2026-08-25)


### Bug Fixes

* **deps:** bump github.com/namecheap/go-namecheap-sdk/v2 from 2.10.1 to 2.10.2 ([#326](https://github.com/namecheap/terraform-provider-namecheap/issues/326)) ([df69f75](https://github.com/namecheap/terraform-provider-namecheap/commit/df69f75706f7e0d1a20e774acad395932c5b15a0))
* **deps:** bump github.com/stretchr/testify from 1.11.1 to 1.12.0 ([#322](https://github.com/namecheap/terraform-provider-namecheap/issues/322)) ([7abe284](https://github.com/namecheap/terraform-provider-namecheap/commit/7abe2843860c15895d09849f3896e79b7d90a41d))
* **deps:** bump golang.org/x/mod to v0.40.0 and update golang.org/x family ([#325](https://github.com/namecheap/terraform-provider-namecheap/issues/325)) ([79e3743](https://github.com/namecheap/terraform-provider-namecheap/commit/79e374399188ca65d7303abfd5fd17e0d5cce9fa))

## [2.9.0](https://github.com/namecheap/terraform-provider-namecheap/compare/v2.8.0...v2.9.0) (2026-08-14)


### Features

* read the full domain info from getInfo and expose privacy, ownership, and rights attributes ([#320](https://github.com/namecheap/terraform-provider-namecheap/issues/320)) ([b42b1a7](https://github.com/namecheap/terraform-provider-namecheap/commit/b42b1a715febbba1e19228290ab2566955735bea))

## [2.8.0](https://github.com/namecheap/terraform-provider-namecheap/compare/v2.7.0...v2.8.0) (2026-08-12)


### Features

* add namecheap_dns_record for per-record DNS management ([#310](https://github.com/namecheap/terraform-provider-namecheap/issues/310)) ([889c7f9](https://github.com/namecheap/terraform-provider-namecheap/commit/889c7f9610d85d9d26981821df6d98bf98e7f406))


### Bug Fixes

* **deps:** bump github.com/go-git/go-git/v5 from 5.19.1 to 5.19.2 ([#316](https://github.com/namecheap/terraform-provider-namecheap/issues/316)) ([1aa7b58](https://github.com/namecheap/terraform-provider-namecheap/commit/1aa7b5839f7dd02fc2e1d3687e5024849bc5008b))
* **deps:** bump github.com/hashicorp/terraform-plugin-log from 0.10.0 to 0.11.0 ([#311](https://github.com/namecheap/terraform-provider-namecheap/issues/311)) ([c06e9b9](https://github.com/namecheap/terraform-provider-namecheap/commit/c06e9b9ba97c2c140711b2c9ba1516cd88265162))

## [2.7.0](https://github.com/namecheap/terraform-provider-namecheap/compare/v2.6.2...v2.7.0) (2026-08-01)


### Features

* add namecheap_account_balance and namecheap_tld_pricing data sources ([#305](https://github.com/namecheap/terraform-provider-namecheap/issues/305)) ([0a0328a](https://github.com/namecheap/terraform-provider-namecheap/commit/0a0328aa1ba3fa76b362dc6bf1b8ead1d98d256d))
* expose retry_base_delay and retry_max_delay provider options ([#307](https://github.com/namecheap/terraform-provider-namecheap/issues/307)) ([6bee275](https://github.com/namecheap/terraform-provider-namecheap/commit/6bee27561aaa40a8de422ccfeab3573211b5d36c))

## [2.6.2](https://github.com/namecheap/terraform-provider-namecheap/compare/v2.6.1...v2.6.2) (2026-07-29)


### Bug Fixes

* **deps:** bump github.com/namecheap/go-namecheap-sdk/v2 from 2.7.0 to 2.7.1 ([#301](https://github.com/namecheap/terraform-provider-namecheap/issues/301)) ([be35f2b](https://github.com/namecheap/terraform-provider-namecheap/commit/be35f2b6ab825d67a2fe409472bd325c6c599798))

## [2.6.1](https://github.com/namecheap/terraform-provider-namecheap/compare/v2.6.0...v2.6.1) (2026-07-22)


### Bug Fixes

* **deps:** bump golang.org/x/text to 0.39.0 and google.golang.org/grpc to 1.82.1 ([#296](https://github.com/namecheap/terraform-provider-namecheap/issues/296)) ([cffc8c5](https://github.com/namecheap/terraform-provider-namecheap/commit/cffc8c52283f5589f08e73a8832789093790c5b5))

## [2.6.0](https://github.com/namecheap/terraform-provider-namecheap/compare/v2.5.0...v2.6.0) (2026-07-13)


### Features

* add namecheap_email_forwarding resource ([#289](https://github.com/namecheap/terraform-provider-namecheap/issues/289)) ([66c8e65](https://github.com/namecheap/terraform-provider-namecheap/commit/66c8e65e4f23fe59b906381532c4d8b298342480))
* warn before OVERWRITE mode deletes unmanaged DNS records ([#288](https://github.com/namecheap/terraform-provider-namecheap/issues/288)) ([825256a](https://github.com/namecheap/terraform-provider-namecheap/commit/825256a2caf948a964f731a3508a68a9f603ca9d))

## [2.5.0](https://github.com/namecheap/terraform-provider-namecheap/compare/v2.4.0...v2.5.0) (2026-07-10)


### Features

* add namecheap_domain_contacts resource (WHOIS contact management) ([#285](https://github.com/namecheap/terraform-provider-namecheap/issues/285)) ([2006dc9](https://github.com/namecheap/terraform-provider-namecheap/commit/2006dc9cde922d646548b76803de1502178ae76d))
* add namecheap_domain, namecheap_domains, namecheap_domain_records data sources ([#286](https://github.com/namecheap/terraform-provider-namecheap/issues/286)) ([517b525](https://github.com/namecheap/terraform-provider-namecheap/commit/517b52573e79813d649d906362782394b1618eda))
* add namecheap_nameserver resource (register personal nameservers) ([#284](https://github.com/namecheap/terraform-provider-namecheap/issues/284)) ([47a251f](https://github.com/namecheap/terraform-provider-namecheap/commit/47a251ff28e10f8bb1e75150fcb1f735b6fd9200))

## [2.4.0](https://github.com/namecheap/terraform-provider-namecheap/compare/v2.3.5...v2.4.0) (2026-07-08)


### Features

* add retry, rate-limit and timeout provider options ([#247](https://github.com/namecheap/terraform-provider-namecheap/issues/247)) ([#265](https://github.com/namecheap/terraform-provider-namecheap/issues/265)) ([ab85e93](https://github.com/namecheap/terraform-provider-namecheap/commit/ab85e93b9b81e9d9b3977168bbd0bd25c8e68784))
* auto-detect client_ip when unset ([#247](https://github.com/namecheap/terraform-provider-namecheap/issues/247)) ([#267](https://github.com/namecheap/terraform-provider-namecheap/issues/267)) ([fd8d33b](https://github.com/namecheap/terraform-provider-namecheap/commit/fd8d33b5e18fd18170e8daf486027861885541eb))
* bridge SDK request logging into tflog ([#247](https://github.com/namecheap/terraform-provider-namecheap/issues/247)) ([#269](https://github.com/namecheap/terraform-provider-namecheap/issues/269)) ([9b4f159](https://github.com/namecheap/terraform-provider-namecheap/commit/9b4f1592f35dda4536a54150e0862c076fa31471))
* map Namecheap API errors to actionable diagnostics ([#247](https://github.com/namecheap/terraform-provider-namecheap/issues/247)) ([#264](https://github.com/namecheap/terraform-provider-namecheap/issues/264)) ([ffaefdf](https://github.com/namecheap/terraform-provider-namecheap/commit/ffaefdf3c6fbf2c9af72f87b97cfb42f35362f0e))


### Bug Fixes

* filter default parking records in readRecordsOverwrite ([#262](https://github.com/namecheap/terraform-provider-namecheap/issues/262)) ([392b847](https://github.com/namecheap/terraform-provider-namecheap/commit/392b847d16c8a5d2fad3208242350fa237a9eb41))

## [2.3.5](https://github.com/namecheap/terraform-provider-namecheap/compare/v2.3.4...v2.3.5) (2026-06-17)


### Bug Fixes

* **deps:** bump hc-install to v0.9.5 and terraform-exec to v0.25.2 ([#231](https://github.com/namecheap/terraform-provider-namecheap/issues/231)) ([3dfc098](https://github.com/namecheap/terraform-provider-namecheap/commit/3dfc09844da3fdde97a7615aec8b904a8b89c320))
