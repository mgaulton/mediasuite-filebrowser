# MediaSuite File Browser fork

This fork is based on `filebrowser/filebrowser` release `v2.63.23`
(`e8a388f840173580116f2743813d03b22286e44e`). The `mediasuite` branch adds
mobile image-fit and swipe navigation, keeps adjacent navigation controls
visible on touch devices, rejects directory overwrite uploads, bounds subtitle
conversion and command WebSocket messages, and applies path deny rules to
resolved symlink targets.

The build must pin the fork source to an immutable commit and use the matching
upstream runtime scripts. Keep the upstream license and notices with the source.

Upstream is archived and no longer provides fixes. Review upstream advisories
and dependency updates, patch applicable issues here, and smoke-test before
deploying updates. CI must run on `mediasuite` pushes. Keep instances
LAN-restricted, keep command execution disabled, and retain authentication and
filesystem permissions appropriate to each library.
