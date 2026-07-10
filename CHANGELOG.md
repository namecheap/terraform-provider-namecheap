# Changelog

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
