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

rebuild public feed
  ./tools -to new_db -c public_feed

rebuild social graph after db migrated
  ./tools -to new_db -c rebuild_social_graph

rebuild user timeline after social graph

for one user:
  ./tools -to new_db -c rebuild_timeline -user yinhm

for all users:
  ./tools -to new_db -c rebuild_timeline

migrate to R2
  ./tools -to new_db -c migrate_media_urls

migrate all to new db
```
./tools -from old_db -to new_db -c db
// ./tools -from old_db -to new_db -c meta # may not needed
./tools -from old_db -to new_db -c sync_meta

./tools -from old_db -to new_db -c public_feed
./tools -to new_db -c rebuild_social_graph
./tools -to new_db -c rebuild_timeline
./tools -to new_db -c migrate_media_urls


./tools -from old_db -to new_db -c profile
./tools -from old_db -to new_db -c debug

```


purge and rebuild meta if wrong oauth:
```
./tools -from old_db -to new_db -c purge_profile
./tools -from old_db -to new_db -c purge_oauth
./tools -from old_db -to new_db -c sync_meta
```


Deploy FriendFeed
=================

Install Fabric 1 locally:
``` 
  python3 -m venv .venv
  . .venv/bin/activate
  pip install 'fabric<2'
  fab --version
```

Verify SSH access:
```
  ssh YourServer
  fab production --list
```

Generate the cookie secret:

```
    openssl rand 40 -base64 > conf/salt.conf
```

First deployment：
```
    fab production bootstrap
    fab production deploy_env
    fab production deploy_config
```

SSL && Nginx

    // fab production deploy_ssl
    fab production deploy_nginx

Routine update

    fab production deploy_db
    // fab production deploy_client
    fab production deploy_web

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
