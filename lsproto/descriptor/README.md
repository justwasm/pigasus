# language_server.protoset

The `exa.language_server_pb` descriptor closure, extracted from the
frontend bundle the clone already embeds
(`internal/rcfrontend/assets/main.js`).

Regenerate with:

    python3 out/ide-language-server/extract_ls_protoset.py \
        internal/lsproto/descriptor/language_server.protoset

The bytes are the ones the shipped Antigravity web UI parses — the
bundle embeds every FileDescriptorProto it needs as a base64 blob
(【一手】). Nothing here is regenerated from a `.proto` source: the
reference's schema is closed-source.
