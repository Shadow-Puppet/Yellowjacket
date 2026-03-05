## [1.1.3](https://github.com/onion-4-dinner/yellowjacket/compare/v1.1.2...v1.1.3) (2026-02-21)

### Bug Fixes

* add typescript as explicit devDependency and auto-install frontend deps in setup ([#70](https://github.com/onion-4-dinner/yellowjacket/issues/70)) ([7316587](https://github.com/onion-4-dinner/yellowjacket/commit/73165877fa79656ab9bc6f60bd8e9e52d6be206c))
* use local tsc binary in pre-commit hook to avoid PATH issues ([#71](https://github.com/onion-4-dinner/yellowjacket/issues/71)) ([6079e55](https://github.com/onion-4-dinner/yellowjacket/commit/6079e558ff913d38c7f1c4aeb52cc09474c4ed20))

## [1.1.2](https://github.com/onion-4-dinner/yellowjacket/compare/v1.1.1...v1.1.2) (2026-02-15)

### Bug Fixes

* r2 upload ([#69](https://github.com/onion-4-dinner/yellowjacket/issues/69)) ([0252466](https://github.com/onion-4-dinner/yellowjacket/commit/0252466f615b4e2fd9694790c6d311a9eac1ccf2))

## [1.1.1](https://github.com/onion-4-dinner/yellowjacket/compare/v1.1.0...v1.1.1) (2026-02-15)

### Bug Fixes

* **ci:** remove build-check job from CI workflow ([#66](https://github.com/onion-4-dinner/yellowjacket/issues/66)) ([42d3f45](https://github.com/onion-4-dinner/yellowjacket/commit/42d3f45d85afa694e9545997af3ff4ac814ad021))

## [1.1.0](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.3...v1.1.0) (2026-02-15)

### Features

* **ci:** upload release artifacts to Cloudflare R2 ([#65](https://github.com/onion-4-dinner/yellowjacket/issues/65)) ([8985084](https://github.com/onion-4-dinner/yellowjacket/commit/89850848cbf7783e5c85348ff18f7cd11d60231a))

## [1.0.3](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.2...v1.0.3) (2026-02-15)

### ⚠ BREAKING CHANGES

* **deps:** update module github.com/evilmartians/lefthook to v2 (#61)
* **deps:** update actions/checkout action to v6 (#45)
* **deps:** update dependency vite to v7 (#53)

### Bug Fixes

* resolve all lint errors and make linting a required CI check ([#62](https://github.com/onion-4-dinner/yellowjacket/issues/62)) ([30b2480](https://github.com/onion-4-dinner/yellowjacket/commit/30b2480df49f57878b0e8c923da6ad8d6fe99416))
* virtual list and cover grid ([#63](https://github.com/onion-4-dinner/yellowjacket/issues/63)) ([7579a76](https://github.com/onion-4-dinner/yellowjacket/commit/7579a768be84225ed46db4e7a90781f3e30e2953))

### Miscellaneous

* **deps:** update actions/checkout action to v6 ([#45](https://github.com/onion-4-dinner/yellowjacket/issues/45)) ([2d6e221](https://github.com/onion-4-dinner/yellowjacket/commit/2d6e22105d2daed1dc5b586c0442e2941949a165))
* **deps:** update dependency vite to v7 ([#53](https://github.com/onion-4-dinner/yellowjacket/issues/53)) ([f0006c4](https://github.com/onion-4-dinner/yellowjacket/commit/f0006c4c4335b60b58cccdd29de4792965e39694))
* **deps:** update module github.com/evilmartians/lefthook to v2 ([#61](https://github.com/onion-4-dinner/yellowjacket/issues/61)) ([e32b217](https://github.com/onion-4-dinner/yellowjacket/commit/e32b2179129ae7f26037697a125710ff7587566d))

## [1.0.2](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.1...v1.0.2) (2026-02-14)

### ⚠ BREAKING CHANGES

* **deps:** update actions/setup-node action to v6 (#48)
* **deps:** update dependency stylelint-config-standard to v40 (#52)
* **deps:** update dependency node to v24 (#51)
* **deps:** update dependency vite-plugin-static-copy to v3 (#54)
* **deps:** update golangci/golangci-lint-action action to v9 (#55)
* **deps:** update amannn/action-semantic-pull-request action to v6 (#50)
* **deps:** update actions/upload-artifact action to v6 (#49)
* **deps:** update actions/setup-go action to v6 (#47)
* **deps:** update actions/download-artifact action to v7 (#46)

### Bug Fixes

* **ci:** use allowedPostUpgradeCommands for Renovate post-upgrade tasks ([#60](https://github.com/onion-4-dinner/yellowjacket/issues/60)) ([0aef483](https://github.com/onion-4-dinner/yellowjacket/commit/0aef483b3cccd0616fd5be2d06d0856b46851d09))

### Miscellaneous

* **deps:** update actions/download-artifact action to v7 ([#46](https://github.com/onion-4-dinner/yellowjacket/issues/46)) ([1910f99](https://github.com/onion-4-dinner/yellowjacket/commit/1910f99cf64e9bdc5ce91e89cab254ecca15d030))
* **deps:** update actions/setup-go action to v6 ([#47](https://github.com/onion-4-dinner/yellowjacket/issues/47)) ([8911fb2](https://github.com/onion-4-dinner/yellowjacket/commit/8911fb2400047cf2f3dfa719edc1d1bf474cdaa5))
* **deps:** update actions/setup-node action to v6 ([#48](https://github.com/onion-4-dinner/yellowjacket/issues/48)) ([d7382fd](https://github.com/onion-4-dinner/yellowjacket/commit/d7382fd8444b6618dbfe991f5f97231528a07f13))
* **deps:** update actions/upload-artifact action to v6 ([#49](https://github.com/onion-4-dinner/yellowjacket/issues/49)) ([a2c644b](https://github.com/onion-4-dinner/yellowjacket/commit/a2c644b00eed83acc0ed38a2eb8c73868b7b79af))
* **deps:** update amannn/action-semantic-pull-request action to v6 ([#50](https://github.com/onion-4-dinner/yellowjacket/issues/50)) ([643ba27](https://github.com/onion-4-dinner/yellowjacket/commit/643ba27f066164aeb47e8d9aaf20fe98b9b69d30))
* **deps:** update dependency node to v24 ([#51](https://github.com/onion-4-dinner/yellowjacket/issues/51)) ([e7d3971](https://github.com/onion-4-dinner/yellowjacket/commit/e7d39711078ce86b0c029f0d03ff81162c5dc28a))
* **deps:** update dependency stylelint-config-standard to v40 ([#52](https://github.com/onion-4-dinner/yellowjacket/issues/52)) ([422aabc](https://github.com/onion-4-dinner/yellowjacket/commit/422aabcc07e9700ff189302b363e13d87c69163a))
* **deps:** update dependency vite-plugin-static-copy to v3 ([#54](https://github.com/onion-4-dinner/yellowjacket/issues/54)) ([77fa643](https://github.com/onion-4-dinner/yellowjacket/commit/77fa6435a5298f58ef83607d99c59b876132c66c))
* **deps:** update golangci/golangci-lint-action action to v9 ([#55](https://github.com/onion-4-dinner/yellowjacket/issues/55)) ([aedb7d1](https://github.com/onion-4-dinner/yellowjacket/commit/aedb7d1e6d204c56c468dd26b340752fd6bfeaeb))

## [1.0.1](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.0...v1.0.1) (2026-02-14)

### Bug Fixes

* resolve Renovate repo detection and pre-push hook hang ([#36](https://github.com/onion-4-dinner/yellowjacket/issues/36)) ([b205889](https://github.com/onion-4-dinner/yellowjacket/commit/b205889128f01e9eb75b607cf7c4034887cda3f4))

## 1.0.0 (2026-02-14)

### Features

* **ci:** add semantic-release pipeline, cross-platform builds, and lefthook git hooks ([caf3e84](https://github.com/onion-4-dinner/yellowjacket/commit/caf3e843af7da37e05da36da5c41b6dc3c53ded1))

### Bug Fixes

* allow library to initialize without config and fix lefthook lint flag ([5a958db](https://github.com/onion-4-dinner/yellowjacket/commit/5a958db16284a74e19c43259757b163b347cda7d))
* **ci:** configure git credentials explicitly for semantic-release PAT ([24f21af](https://github.com/onion-4-dinner/yellowjacket/commit/24f21af8350227e77fc1fef9243c238e6417aca0))
* **ci:** fix golangci-lint version, skip player test in CI, remove standalone frontend build ([7317e09](https://github.com/onion-4-dinner/yellowjacket/commit/7317e093a7f92651ab65b2f83381d02105bdc0df))
* **ci:** resolve CI failures for Go checks, codegen, and frontend type-checking ([d4f9361](https://github.com/onion-4-dinner/yellowjacket/commit/d4f936143ac75fbf3247cdbe2113bd89b0795d83))
* **ci:** use PAT for semantic-release to trigger build workflow ([68d41c0](https://github.com/onion-4-dinner/yellowjacket/commit/68d41c0ff22fede57acab7a2bfed42df7814bb90))
* rename downloaded artifacts to platform-specific names for release ([e3bda0e](https://github.com/onion-4-dinner/yellowjacket/commit/e3bda0e2fc7700fad382cabe00aeb46f91fbb0a0))
* resolve frontend build failures in CI ([330a53c](https://github.com/onion-4-dinner/yellowjacket/commit/330a53c9f4b1292840ad0f75479b76b3d429c954))
* trigger build workflow from release event instead of tag push ([47772f7](https://github.com/onion-4-dinner/yellowjacket/commit/47772f73cc04093c55414bf20ebe2ef442418d19))
* use path.Join for embed.FS paths to fix Windows build ([672fe24](https://github.com/onion-4-dinner/yellowjacket/commit/672fe24ee99debf4a394fff7eec55f17b0e44476))

## 1.0.0 (2026-02-14)

### Features

* **ci:** add semantic-release pipeline, cross-platform builds, and lefthook git hooks ([caf3e84](https://github.com/onion-4-dinner/yellowjacket/commit/caf3e843af7da37e05da36da5c41b6dc3c53ded1))

### Bug Fixes

* allow library to initialize without config and fix lefthook lint flag ([5a958db](https://github.com/onion-4-dinner/yellowjacket/commit/5a958db16284a74e19c43259757b163b347cda7d))
* **ci:** configure git credentials explicitly for semantic-release PAT ([24f21af](https://github.com/onion-4-dinner/yellowjacket/commit/24f21af8350227e77fc1fef9243c238e6417aca0))
* **ci:** fix golangci-lint version, skip player test in CI, remove standalone frontend build ([7317e09](https://github.com/onion-4-dinner/yellowjacket/commit/7317e093a7f92651ab65b2f83381d02105bdc0df))
* **ci:** resolve CI failures for Go checks, codegen, and frontend type-checking ([d4f9361](https://github.com/onion-4-dinner/yellowjacket/commit/d4f936143ac75fbf3247cdbe2113bd89b0795d83))
* **ci:** use PAT for semantic-release to trigger build workflow ([68d41c0](https://github.com/onion-4-dinner/yellowjacket/commit/68d41c0ff22fede57acab7a2bfed42df7814bb90))
* resolve frontend build failures in CI ([330a53c](https://github.com/onion-4-dinner/yellowjacket/commit/330a53c9f4b1292840ad0f75479b76b3d429c954))
* trigger build workflow from release event instead of tag push ([47772f7](https://github.com/onion-4-dinner/yellowjacket/commit/47772f73cc04093c55414bf20ebe2ef442418d19))
* use path.Join for embed.FS paths to fix Windows build ([672fe24](https://github.com/onion-4-dinner/yellowjacket/commit/672fe24ee99debf4a394fff7eec55f17b0e44476))

## [1.0.3](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.2...v1.0.3) (2026-02-14)

### Bug Fixes

* use path.Join for embed.FS paths to fix Windows build ([672fe24](https://github.com/onion-4-dinner/yellowjacket/commit/672fe24ee99debf4a394fff7eec55f17b0e44476))

## [1.0.2](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.1...v1.0.2) (2026-02-14)

### Bug Fixes

* resolve frontend build failures in CI ([330a53c](https://github.com/onion-4-dinner/yellowjacket/commit/330a53c9f4b1292840ad0f75479b76b3d429c954))

## [1.0.1](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.0...v1.0.1) (2026-02-14)

### Bug Fixes

* allow library to initialize without config and fix lefthook lint flag ([5a958db](https://github.com/onion-4-dinner/yellowjacket/commit/5a958db16284a74e19c43259757b163b347cda7d))

## 1.0.0 (2026-02-14)

### Features

* **ci:** add semantic-release pipeline, cross-platform builds, and lefthook git hooks ([caf3e84](https://github.com/onion-4-dinner/yellowjacket/commit/caf3e843af7da37e05da36da5c41b6dc3c53ded1))

### Bug Fixes

* **ci:** configure git credentials explicitly for semantic-release PAT ([24f21af](https://github.com/onion-4-dinner/yellowjacket/commit/24f21af8350227e77fc1fef9243c238e6417aca0))
* **ci:** fix golangci-lint version, skip player test in CI, remove standalone frontend build ([7317e09](https://github.com/onion-4-dinner/yellowjacket/commit/7317e093a7f92651ab65b2f83381d02105bdc0df))
* **ci:** resolve CI failures for Go checks, codegen, and frontend type-checking ([d4f9361](https://github.com/onion-4-dinner/yellowjacket/commit/d4f936143ac75fbf3247cdbe2113bd89b0795d83))
* **ci:** use PAT for semantic-release to trigger build workflow ([68d41c0](https://github.com/onion-4-dinner/yellowjacket/commit/68d41c0ff22fede57acab7a2bfed42df7814bb90))

## [1.0.2](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.1...v1.0.2) (2026-02-14)

### Bug Fixes

* **ci:** fix golangci-lint version, skip player test in CI, remove standalone frontend build ([7317e09](https://github.com/onion-4-dinner/yellowjacket/commit/7317e093a7f92651ab65b2f83381d02105bdc0df))

## [1.0.1](https://github.com/onion-4-dinner/yellowjacket/compare/v1.0.0...v1.0.1) (2026-02-14)

### Bug Fixes

* **ci:** resolve CI failures for Go checks, codegen, and frontend type-checking ([d4f9361](https://github.com/onion-4-dinner/yellowjacket/commit/d4f936143ac75fbf3247cdbe2113bd89b0795d83))

## 1.0.0 (2026-02-14)

### Features

* **ci:** add semantic-release pipeline, cross-platform builds, and lefthook git hooks ([caf3e84](https://github.com/onion-4-dinner/yellowjacket/commit/caf3e843af7da37e05da36da5c41b6dc3c53ded1))

### Bug Fixes

* **ci:** configure git credentials explicitly for semantic-release PAT ([24f21af](https://github.com/onion-4-dinner/yellowjacket/commit/24f21af8350227e77fc1fef9243c238e6417aca0))
* **ci:** use PAT for semantic-release to trigger build workflow ([68d41c0](https://github.com/onion-4-dinner/yellowjacket/commit/68d41c0ff22fede57acab7a2bfed42df7814bb90))

## 1.0.0 (2026-02-14)

### Features

* **ci:** add semantic-release pipeline, cross-platform builds, and lefthook git hooks ([caf3e84](https://github.com/onion-4-dinner/yellowjacket/commit/caf3e843af7da37e05da36da5c41b6dc3c53ded1))

### Bug Fixes

* **ci:** configure git credentials explicitly for semantic-release PAT ([24f21af](https://github.com/onion-4-dinner/yellowjacket/commit/24f21af8350227e77fc1fef9243c238e6417aca0))
* **ci:** use PAT for semantic-release to trigger build workflow ([68d41c0](https://github.com/onion-4-dinner/yellowjacket/commit/68d41c0ff22fede57acab7a2bfed42df7814bb90))

## 1.0.0 (2026-02-14)

### Features

* **ci:** add semantic-release pipeline, cross-platform builds, and lefthook git hooks ([caf3e84](https://github.com/onion-4-dinner/yellowjacket/commit/caf3e843af7da37e05da36da5c41b6dc3c53ded1))

### Bug Fixes

* **ci:** use PAT for semantic-release to trigger build workflow ([68d41c0](https://github.com/onion-4-dinner/yellowjacket/commit/68d41c0ff22fede57acab7a2bfed42df7814bb90))

## 1.0.0 (2026-02-14)

### Features

* **ci:** add semantic-release pipeline, cross-platform builds, and lefthook git hooks ([caf3e84](https://github.com/onion-4-dinner/yellowjacket/commit/caf3e843af7da37e05da36da5c41b6dc3c53ded1))
