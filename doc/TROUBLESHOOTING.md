# Troubleshooting

## Auth errors when trying to use git from inside a container
*Homebrew openssh note*: I haven't tested `sand` with homebrew's openssh, but there appear to be some problems using its ssh-agent in combination with Apple keychain-managed keys. See [this issue](https://github.com/banksean/sand/issues/54).

If you are using the ssh that ships with macOS, and try to run git commands that require authentication over ssh, you might see an error message like:
```sh
[container shell]> git pull
git@github.com: Permission denied (publickey).
fatal: Could not read from remote repository.

Please make sure you have the correct access rights
and the repository exists.
```

This is likely due to `ssh-agent` not being aware of the ssh key you use for github. If you are using macOS's ssh support, you can add your github key to ssh-agent with this command:

```sh
ssh-add --apple-use-keychain ~/.ssh/<your github ssh key>
```

