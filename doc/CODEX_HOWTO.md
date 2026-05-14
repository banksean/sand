# How to use `sand` to sandbox your codex sessions:

- Visit https://platform.openai.com/settings/organization/api-keys
- Click the "Create new secret key" button
- Select a project (e.g. "Default project")
- Optional: specify Permissions (e.g. "Read Only")
- Click "Create secret key"
- Copy the displayed secret key value
- Save it in your .env file as `OPENAI_API_KEY=sk-proj-...`

Run this:

```sh
[macos host shell] % sand new -a codex
```

You should now be in a terminal session running codex in a new sandbox container.

If you are prompted like so:
```
Welcome to Codex, OpenAI's command-line coding agent

  Sign in with ChatGPT to use Codex as part of your paid plan
  or connect an API key for usage-based billing

  1. Sign in with ChatGPT
     Usage included with Plus, Pro, Business, and Enterprise plans

  2. Sign in with Device Code
     Sign in from another device with a one-time code

> 3. Provide your own API key
     Pay for what you use

  Press enter to continue
```

Select option 3 and hit enter. You should see your OPENAI_API_KEY pre-populated in the
"API key" box.  Hit enter to start the codex session authenticated with your API key.

```
 Welcome to Codex, OpenAI's command-line coding agent

> Use your own OpenAI API key for usage-based billing

  Paste or type your API key below. It will be stored locally in auth.json.

  Detected OPENAI_API_KEY environment variable.
  Paste a different key if you prefer to use another account.

╭API key───────────────────────────────────────────────────────────────────╮
│sk-proj-...                                                               │
╰──────────────────────────────────────────────────────────────────────────╯
  Press enter to save
  Press esc to go back
```