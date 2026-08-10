A FriendFeed Clone
==================

  This project created by a handful of FriendFeed enthusiasts, as FriendFeed
  was shutting down[1].

Golang Env
==========

    sudo mkdir -p /srv/gopath/bin
    echo "export GOPATH=/srv/gopath" >> ~/.bashrc
    echo "export PATH=$GOPATH/bin:$PATH" >> ~/.bashrc

    cd ~/src && curl -L https://godeb.s3.amazonaws.com/godeb-amd64.tar.gz \
         | tar zx --strip 1 && ./godeb install 1.26.4
    mkdir /srv/gopath/bin && mv godeb /srv/gopath/bin/

    sudo apt-get install git-core -y
    sudo apt-get install imagemagick -y


Server Config
============

  Config files include media, twitter, gauth file, etc

    cp conf/example.config.json conf/config.json

  Change config.json according to your project.

Google OAUTH2
============

 * console.developers.google.com -> APIs & auth -> Consent Screen -> must have
   email 
 * Creadentials -> Create new client ID
 * Place json key file to conf/gauth.json

Web Dev
=======

build js

    # Install Node.js 24 LTS (the exact version is in .nvmrc)
    cd httpd/app
    corepack enable pnpm
    pnpm install --frozen-lockfile
    pnpm start

Start develop

    cd httpd
    go get .
    go build; ./httpd -f=../conf/gauth.json -d

Or use Gin(recommend)
        
    go get github.com/codegangsta/gin
    export DEBUG=1
    export RPC="localhost:8901"
    export CONFIG_FILE=/srv/ff/config.json
    gin -p 8080

migrate
=======

  See [docs/db_migration.md](docs/db_migration.md).

  Profile identity, rename redirects and rename-map operations are documented
  in [docs/profile_rename.md](docs/profile_rename.md).


Deploy FriendFeed
=================

Install Fabric 3 locally:
``` 
  uv venv .venv
  uv pip install -r requirements.txt
  uv run --no-project fab --version
```

Verify SSH access:
```
  ssh YourServer
  uv run --no-project fab --list
```

Generate the cookie secret:

```
    openssl rand 40 -base64 > conf/salt.conf
```

First deployment：
```
    uv run --no-project fab production bootstrap
    uv run --no-project fab production deploy_env
    uv run --no-project fab production deploy_config
```

SSL && Nginx

    # uv run --no-project fab production deploy_ssl
    uv run --no-project fab production deploy_nginx

Routine update

    uv run --no-project fab production deploy_db
    # uv run --no-project fab production deploy_client
    uv run --no-project fab production deploy_web

Systemd logs
============

`ffdb` and `ffweb` write stdout and stderr to journald:

    journalctl -f -u ffdb.service -u ffweb.service

Useful service and log commands:

    systemctl status ffdb.service ffweb.service
    systemctl restart ffdb.service ffweb.service
    journalctl -u ffdb.service --since today
    journalctl -u ffweb.service -n 200 --no-pager

For persistent, bounded logs, configure `/etc/systemd/journald.conf`:

    [Journal]
    Storage=persistent
    SystemMaxUse=2G
    MaxRetentionSec=30day
    Compress=yes

Then apply it:

    sudo mkdir -p /var/log/journal
    sudo systemctl restart systemd-journald
