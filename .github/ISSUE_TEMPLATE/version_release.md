---
name: Version Release
about: Checklist to follow when shipping a new release.
title: 'Release <version> Checklist'
labels: ''
assignees: ''
---

- [ ] Verify CI is passing on the target branch
- [ ] Run the [Prepare Release](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/blob/main/docs/RELEASE.md#1-run-the-prepare-release-workflow) workflow
- [ ] Review and approve the [release PR](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/blob/main/docs/RELEASE.md#2-review-the-release-pr)
- [ ] Merge the release PR
- [ ] Verify release artifacts on the [Releases page](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/releases)
- [ ] [Sync website documentation](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/blob/main/docs/RELEASE.md#post-release)

<sub>**Tip**: [React](https://github.blog/news-insights/product-news/add-reactions-to-pull-requests-issues-and-comments/) with 👍 to help prioritize this issue. Please use comments to provide useful context, avoiding `+1` or `me too`, to help us triage it. Learn more [here](https://opentelemetry.io/community/end-user/issue-participation/).</sub>
