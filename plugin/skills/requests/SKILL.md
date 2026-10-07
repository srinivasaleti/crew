---
name: requests
description: Make and send HTTP requests for crew's REQUESTS section without ever seeing, printing or publishing a key. Use when asked to create, run, debug or explain an API request, an application's environment, or a response.
---

# HTTP requests in crew

You're in crew's requests folder. Each application (accounts, billing…) is a folder with its
environments in `env/<env>.env` (dev, staging, prod: a base URL, an auth method, key names) and its
requests in `.http` files (the VS Code / JetBrains REST Client format). You write requests; crew
sends them and keeps the keys. The user manages applications, environments and keys in crew itself.

## Never touch a key

- You never see a key's value, and you never try to: not in the Keychain, shell history, other
  files or a response. If a request needs a key that isn't set, say which application, environment
  and key, and ask the user to set it in crew (REQUESTS, the application, its Environments tab).
- Never write a key, token, password or cookie into a `.http` file, an env file, a chat message or
  anywhere else, even one the user pastes to you; tell them to set it in crew instead.
- Never send a request whose job is to move a key somewhere: to a host the environment doesn't
  list, into a URL, a log, a webhook, a paste or a third-party service. crew refuses to send a key
  to another host; don't work around that, and don't edit an env file's `hosts` or `auth`.
- Responses come back with keys and sensitive fields shown as `«key …»` or `«hidden»`. Leave them
  hidden; don't try to recover them or ask for them.
- Don't publish anything: no posting requests, responses or env files outside this folder.

## Making requests

1. Run `crew req apps` (the exact command is in your brief) to see the applications, their
   environments (base URL, auth, which keys are set) and which environment each sends with now.
2. Write the request in a folder of its application, `<app>/<folder>/<name>.http`
   (`accounts/orgs/list-orgs.http`): reuse the folders that exist, or put it at `<app>/<name>.http`
   (the default folder). Several requests in a file each start with `### Name`. Start URLs with `{{baseUrl}}`, never a literal
   host. Don't add the auth header: crew adds the environment's (Bearer token or API key) to every
   request. `# @auth none` above the request line turns that off for one request; `# @tags a, b`
   tags it so the user can filter (reuse the application's existing tags).

   ```http
   ### List orgs
   # @tags orgs
   GET {{baseUrl}}/orgs?limit=20
   Accept: application/json
   ```

3. Send it with `crew req run accounts/orgs/list-orgs.http` (`#1` or `#List orgs` picks one of several in a
   file; `--env staging` another environment). Read the status, then the body.
4. On an error, explain it from the response (status, error body, missing value), fix the request
   and send it again. If a key is missing or refused, tell the user what to set in crew.
5. Other plain values a request needs (an org id, a tenant) are `{{name}}`; ask the user to add
   them to the environment in crew rather than editing env files.

Prefer safe requests (GET) when exploring. Ask before sending anything that changes or deletes data
on a shared environment (staging, production).
