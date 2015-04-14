A FriendFeed Clone
==================

  This project created by a handful of FriendFeed enthusiasts, as FriendFeed
  was shutting down[1].

Google Cloud
============

You may need to create bucket in console.developers.google.com.

Init Google Cloud

    curl https://sdk.cloud.google.com | bash

Login, eg:

    gcloud auth login
    gcloud config set project "GCEAppId"

Set storage bucket to public

    gsutil defacl ch -g AllUsers:R gs://GCSBucketId

SSH

    gcloud compute --project "GCSAppId" ssh --zone "us-central1-f" "instance-1"

Attach Disk
==========
    gcloud compute zones list
    gcloud compute regions describe us-central1
    gcloud compute regions list

    // create then select regoin
    gcloud compute disks create ffdb --size 500GB
    gcloud compute instances attach-disk "instance-1" --disk ffdb --zone "us-central1-f"

  ssh to instance

    sudo mkdir /mnt/tmp
    sudo /usr/share/google/safe_format_and_mount -m "mkfs.ext4 -F" /dev/sdb /mnt/tmp

    sudo sh -c 'cat "/dev/sdb /srv ext4 noatime 0 0" >> /etc/fstab'
    sudo mount /srv

  WARN: Google Cloud Disk IOPS performance limits grow linearly with the size
  of the persistent disk volume, you may not want to create disk <=200GB.

Firewall
========

If you need remote clients, you need to create gcloud firewall rules:

    gcloud compute firewall-rules list
    gcloud compute firewall-rules create ffapi --allow tcp:8901 --source-tags=instance-1 --source-ranges=REMOTE_IP/32 --description="ffapi"


Golang Env
==========

    sudo mkdir -p /srv/gopath/bin
    echo "export GOPATH=/srv/gopath" >> ~/.bashrc
    echo "export PATH=$GOPATH/bin:$PATH" >> ~/.bashrc

    cd ~/src && curl -L https://godeb.s3.amazonaws.com/godeb-amd64.tar.gz \
         | tar zx --strip 1 && ./godeb install 1.4.2
    mkdir /srv/gopath/bin && mv godeb /srv/gopath/bin/

    sudo apt-get install git-core -y
    sudo apt-get install imagemagick -y


Server Config
============

  Config files include media, twitter, gauth file, etc

    cp conf/example.config.json conf/config.json

  Change config.json according to your project.

Media
====

All medias will be archived to Google Cloud Storage if it was from friendfeed.

RocksDB
=======

    fab production deploy_env

Google OAUTH2
============

 * console.developers.google.com -> APIs & auth -> Consent Screen -> must have
   email 
 * Creadentials -> Create new client ID
 * Place json key file to conf/gauth.json

Web Dev
=======

    cd httpd
    go get .

Start develop

    go build; ./httpd -f=../conf/gauth.json -d

Or use Gin
        
    go get github.com/codegangsta/gin
    export DEBUG=1
    export RPC="localhost:8901"
    export CONFIG_FILE=/srv/ff/config.json
    gin -p 8080

Deploy FriendFeed
=================

You need fabric in your local machine if you run fab.

    sudo apt-get install -y fabric


Setup once

  * Create Google Cloud Engine
  * Create Google Cloud Storage Bucket, config file save to conf/gcs.json
  * Media config as described in previous section.

```
    openssl rand 40 -base64 > conf/salt.conf
    fab production bootstrap
    fab production deploy_env
    fab production deploy_config
```

SSL && Nginx

    fab production deploy_ssl
    fab production deploy_nginx

Routine update

    fab production deploy
    fab production deploy_client
    fab production deploy_web

deploy_client only start one client, if you need more, start ffclient manually.


Notice: The FriendFeed clone project is not affiliated with FriendFeed.

[1] http://blog.friendfeed.com/2015/03/dear-friendfeed-community-were.html
