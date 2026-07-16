#!/usr/bin/env python
# -*- coding: utf-8 -*-
import os
import sys

from copy import copy
from os.path import dirname, expanduser, join
from fabric.api import *
from fabric.contrib.files import upload_template, append, exists
from fabric.context_managers import cd, shell_env

from fabric.colors import _wrap_with, green

green_bg = _wrap_with('42')
red_bg = _wrap_with('41')


# env.key_filename = expanduser('~/.ssh/google_compute_engine')
env.use_ssh_config = True

@task
def production():
    env.hostname = 'ff1'
    env.user = 'root'

    #  name of your project - no spaces, no special chars
    env.project = 'ffdb'
    #  hg repository of your project
    env.repository = 'git@github.com:yinhm/ffdb.git'
    #  type of repository (git or hg)
    env.repository_type = 'git'
    #  hosts to deploy your project, users must be sudoers
    env.hosts = ['linode', ]
    # additional packages to be installed on the server
    env.additional_packages = [
        #'mercurial',
    ]

    #  system user, owner of the processes and code on your server
    #  the user and it's home dir will be created if not present
    env.runner_user = 'www-data'
    # user group
    env.runner_group = env.runner_user
    #  the code of your project will be located here
    env.deploy_root = '/srv'
    #  project path
    env.project_path = join(env.deploy_root, env.project)
    env.go_path = '/srv/gopath'
    #  project source under go_path
    env.code_root = join(env.go_path, 'src/github.com/yinhm/friendfeed')
    env.httpcache_path = join(env.project_path, 'httpcache')

    env.ffclient_logfile = join(env.deploy_root, 'logs', 'ffclient.log')
    env.ffweb_bind = "127.0.0.1:8080"

    env.nginx_https = True
    env.nginx_server_name = 'friendfeed.me'
    env.nginx_client_max_body_size = 200
    
def test_if(func, *args, **kwargs):
    """
    Run a function and return True if it succeeds,
    False if it fails. This is good for testing the
    environment on remote hosts.
    """
    condition = False
    with settings(hide('everything'), warn_only=True):
        output = func(*args, **kwargs)
        if not output.return_code: condition = True
    return condition

@task
def line_in_file(line, filename):
    """Use a remote grep to see if a particular line is in a file.
    Return True or False.
    """
    grep_in_file = lambda: run('grep "%s" "%s"' % (line, filename))
    result = test_if(grep_in_file)
    if result:
        puts('Value already in file:\n{}'.format(line))
    return result

@task
def locale():
    sudo("echo 'LANG=\"en_US.UTF-8\"' > /etc/default/locale")
    sudo("echo 'LANGUAGE=\"en_US:en\"' >> /etc/default/locale")
    sudo("locale-gen en_US.UTF-8")
    sudo("update-locale en_US.UTF-8")
    sudo("locale-gen zh_CN.UTF-8")


@task
def bootstrap():
    sudo("apt-get update")
    sudo("apt-get -y install git-core")
    sudo("apt-get -y install imagemagick")
    sudo("apt-get -y install unzip")
    sudo("apt-get -y install tmux")
    sudo("apt-get -y install nginx")
    sudo("apt-get -y install nodejs")

    # sudo ("apt-get -y install debhelper libsnappy-dev libgflags-dev libjemalloc-dev libbz2-dev zlib1g-dev")
    # sudo("sudo apt-get -y install devscripts")
    # sudo('git config --global url."git@github.com:".insteadOf "https://github.com/"')


@task
def deploy_env():
    build_path = "/srv/build"
    sudo('mkdir -p %s' % build_path)
    
    with cd(build_path):
        sudo("curl -L https://godeb.s3.amazonaws.com/godeb-amd64.tar.gz | tar zx")
        sudo("./godeb install 1.26.4")


@task
def deploy_config():
    if not exists(env.project_path):
        sudo('mkdir -p %s' % env.project_path)

    context = copy(env)
    template = 'conf/config.json'
    key_path = join(env.project_path, "config.json")
    upload_template(template, key_path,
                    context=context, backup=False, use_sudo=True)
    sudo('chown %s:%s %s' % (env.runner_user, env.runner_group, key_path))
    sudo('chmod 600 %s' % (key_path))

@task
def deploy_db():
    go_path = env.go_path
    db_path = join(env.project_path, "db")
    code_root = env.code_root
    
    if not exists(code_root):
        sudo('mkdir -p %s' % env.project_path)
        sudo('mkdir -p %s/bin' % go_path)
        sudo('mkdir -p %s' % dirname(code_root))
        sudo('chown %s:%s %s -R' % (env.runner_user, env.runner_user, go_path))

    if not exists(db_path):
        sudo('mkdir -p %s' % db_path)
        sudo('chown %s:%s %s' % (env.runner_user, env.runner_group, db_path))

    template = 'conf/ffdb.service'
    context = copy(env)
    upload_template(template, '/etc/systemd/system/ffdb.service',
                    context=context, backup=False, use_sudo=True)

    with shell_env(GOPATH=go_path):
        if not exists(code_root):
            run('git clone %s %s' % (env.repository, code_root))

        with cd(code_root):
            run('git checkout master && git pull')
            # run("go get -u -f")
            run("go get .")
            run("go install")

    sudo('chown %s:%s %s/bin/ffdb' % (env.runner_user, env.runner_group, go_path))

    sudo("systemctl daemon-reload")
    sudo("systemctl enable ffdb.service")
    sudo("systemctl restart ffdb.service")


@task
def deploy_client():
    go_path = env.go_path
    db_path = env.httpcache_path
    code_root = env.code_root

    if not exists(code_root):
        sudo('mkdir -p %s' % env.project_path)
        sudo('mkdir -p %s/bin' % go_path)
        sudo('mkdir -p %s' % dirname(code_root))
        sudo('mkdir -p %s' % dirname(env.ffclient_logfile))

        sudo('chown %s:%s %s -R' % (env.runner_user, env.runner_user, go_path))

    if not exists(db_path):
        sudo('mkdir -p %s' % db_path)
        sudo('chown %s:%s %s' % (env.runner_user, env.runner_group, db_path))

    sudo('chown %s %s' % (env.runner_user, dirname(env.ffclient_logfile)))
    sudo('chmod -R 775 %s' % dirname(env.ffclient_logfile))

    template = 'conf/ffclient.conf'
    context = copy(env)
    upload_template(template, '/etc/init/ffclient.conf',
                    context=context, backup=False, use_sudo=True)

    with shell_env(GOPATH=go_path):
        if not exists(code_root):
            run('git clone %s %s' % (env.repository, code_root))

        with cd(join(code_root, 'client')):
            run('git checkout master && git pull')
            # run("go get -u -f")
            run("go get .")
            run("go build && mv client %s/bin/ffclient" % go_path)

    with settings(warn_only=True):
        sudo("stop ffclient")

    sudo("start ffclient")



@task
def deploy_web():
    go_path = env.go_path
    db_path = env.httpcache_path
    code_root = env.code_root

    web_path = join(env.project_path, "www")
    if not exists(web_path):
        sudo('mkdir -p %s' % web_path)
        sudo('chown %s:%s %s' % (env.runner_user, env.runner_group, web_path))


    # key file
    template = 'conf/gauth.json'
    context = copy(env)
    key_path = join(env.project_path, "gauth.json")
    upload_template(template, key_path,
                    context=context, backup=False, use_sudo=True)
    sudo('chown %s:%s %s' % (env.runner_user, env.runner_group, key_path))
    sudo('chmod 600 %s' % (key_path))
    

    template = 'conf/ffweb.service'
    context = copy(env)
    context.salt = open('conf/salt.conf').read().strip()
    context.config_file = join(env.project_path, "config.json")
    context.web_path = web_path
    context.www_public_path = web_path
    upload_template(template, '/etc/systemd/system/ffweb.service',
                    context=context, backup=False, use_sudo=True)

    with shell_env(GOPATH=go_path):
        if not exists(code_root):
            run('git clone %s %s' % (env.repository, code_root))

        with cd(code_root):
            run('git reset --hard && git checkout master && git pull')
            run("cd %s/httpd/app && corepack enable pnpm && pnpm install --frozen-lockfile && pnpm run build" % code_root)
            run("cd %s/httpd && go get ." % code_root)
            run("cd httpd && go build")

        bin_path = join(code_root, 'httpd', 'httpd')
        web_bin_path = join(go_path, "bin/ffweb")
        sudo("mv %s %s" % (bin_path, web_bin_path))
        sudo('chown %s:%s %s -R' % (env.runner_user, env.runner_group, web_bin_path))
    
    sudo("systemctl daemon-reload")
    sudo("systemctl enable ffweb.service")
    sudo("systemctl restart ffweb.service")


# friendfeed.me
@task
def deploy_ssl():
    """Two-step ssl deploy
    First gen server csr, then regen keys.
    """
    domain = env.nginx_server_name
    ssl_path = "/srv/ssl"
    sudo('mkdir -p %s' % ssl_path)

    key_file = '/srv/ssl/%s.key' % domain
    csr_file = '/srv/ssl/%s.csr' % domain
    crt_file = '/srv/ssl/%s.crt' % domain

    # Organization and Organization Unit: NA
    # FQDN must equal to domain name

    if not exists(csr_file):
        sudo("openssl req -nodes -newkey rsa:2048 -keyout %s -out %s" % (key_file, csr_file))
        exit("put keys in dir when run this again")


    # after
    # cat friendfeed_me.crt COMODORSADomainValidationSecureServerCA.crt COMODORSAAddTrustCA.crt AddTrustExternalCARoot.crt > /srv/ssl/friendfeed.me.crt

    sudo('chmod 400 %s' % key_file)
    sudo('chmod 400 %s' % crt_file)
    
@task
def deploy_nginx():
    '''deploy nginx '''
    web_path = join(env.project_path, "www")

    # https handled by cloudflare
    template = 'conf/nginx_http.conf'
    nginx_conf_file = '/etc/nginx/sites-enabled/friendfeed.conf'

    context = copy(env)
    context.www_public_path = web_path
    upload_template(template, nginx_conf_file,
                    context=context, backup=False, use_sudo=True)

    with settings(hide('running', 'stdout', 'stderr', 'warnings'), warn_only=True):
        res = sudo('nginx -t -c /etc/nginx/nginx.conf')
    if 'test failed' in res:
        abort(red_bg('NGINX configuration test failed! Please review your parameters.'))

    sudo('nginx -s reload')
