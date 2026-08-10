"""FriendFeed deployment tasks for Fabric 3."""

from io import StringIO
from pathlib import Path
from shlex import quote
from types import SimpleNamespace
from uuid import uuid4

from fabric import Config, Connection, task
from invoke import Exit

env = SimpleNamespace()
_connection = None


@task(name="production")
def production(_ctx):
    """Select the production deployment target."""
    global _connection

    env.hostname = "ff1"
    env.user = "root"
    env.project = "ffdb"
    env.repository = "git@github.com:yinhm/ffdb.git"
    env.repository_type = "git"
    env.hosts = ["linode"]
    env.additional_packages = []
    env.runner_user = "www-data"
    env.runner_group = env.runner_user
    env.deploy_root = "/srv"
    env.project_path = f"{env.deploy_root}/{env.project}"
    env.go_path = "/srv/gopath"
    env.code_root = f"{env.go_path}/src/github.com/yinhm/friendfeed"
    env.httpcache_path = f"{env.project_path}/httpcache"
    env.ffclient_logfile = f"{env.deploy_root}/logs/ffclient.log"
    env.ffweb_bind = "127.0.0.1:8080"
    env.nginx_https = True
    env.nginx_server_name = "friendfeed.me"
    env.nginx_client_max_body_size = 200

    config = Config(overrides={"load_ssh_configs": True})
    _connection = Connection(env.hosts[0], user=env.user, config=config)


def _conn():
    if _connection is None:
        raise Exit("select an environment first, for example: fab production deploy_db")
    return _connection


def _exists(path):
    return _conn().run(f"test -e {quote(path)}", hide=True, warn=True).ok


def _template_context(**values):
    context = vars(env).copy()
    context.update(values)
    return context


def _upload_template(template_path, remote_path, context):
    """Render a percent-format template and install it through sudo."""
    rendered = Path(template_path).read_text(encoding="utf-8") % context
    temporary = f"/tmp/ffdb-deploy-{uuid4().hex}"
    conn = _conn()
    try:
        conn.put(StringIO(rendered), remote=temporary)
        conn.sudo(f"install -m 0644 {quote(temporary)} {quote(remote_path)}")
    finally:
        conn.run(f"rm -f {quote(temporary)}", hide=True, warn=True)


@task(name="line_in_file")
def line_in_file(_ctx, line, filename):
    """Return whether a remote file contains a fixed string."""
    result = _conn().run(
        f"grep -F -- {quote(line)} {quote(filename)}", hide=True, warn=True
    )
    if result.ok:
        print(f"Value already in file:\n{line}")
    return result.ok


@task(name="locale")
def locale(_ctx):
    conn = _conn()
    conn.sudo("sh -c 'echo LANG=\\\"en_US.UTF-8\\\" > /etc/default/locale'")
    conn.sudo("sh -c 'echo LANGUAGE=\\\"en_US:en\\\" >> /etc/default/locale'")
    conn.sudo("locale-gen en_US.UTF-8")
    conn.sudo("update-locale LANG=en_US.UTF-8 LANGUAGE=en_US:en")
    conn.sudo("locale-gen zh_CN.UTF-8")


@task(name="bootstrap")
def bootstrap(_ctx):
    conn = _conn()
    conn.sudo("apt-get update")
    conn.sudo("apt-get -y install git-core imagemagick unzip tmux nginx nodejs")


@task(name="deploy_env")
def deploy_env(_ctx):
    conn = _conn()
    build_path = "/srv/build"
    conn.sudo(f"mkdir -p {quote(build_path)}")
    with conn.cd(build_path):
        conn.sudo("curl -L https://godeb.s3.amazonaws.com/godeb-amd64.tar.gz | tar zx")
        conn.sudo("./godeb install 1.26.4")


@task(name="deploy_config")
def deploy_config(_ctx):
    conn = _conn()
    if not _exists(env.project_path):
        conn.sudo(f"mkdir -p {quote(env.project_path)}")

    key_path = f"{env.project_path}/config.json"
    _upload_template("conf/config.json", key_path, _template_context())
    conn.sudo(f"chown {env.runner_user}:{env.runner_group} {quote(key_path)}")
    conn.sudo(f"chmod 600 {quote(key_path)}")


def _update_and_build(commands, clean=False):
    conn = _conn()
    with conn.prefix(f"export GOPATH={quote(env.go_path)}"):
        if not _exists(env.code_root):
            conn.run(f"git clone {quote(env.repository)} {quote(env.code_root)}")

        with conn.cd(env.code_root):
            if clean:
                conn.run("git reset --hard && git checkout master && git pull")
            else:
                conn.run("git checkout master && git pull")
            for command in commands:
                conn.run(command)


@task(name="deploy_db")
def deploy_db(_ctx):
    conn = _conn()
    db_path = f"{env.project_path}/db"

    if not _exists(env.code_root):
        conn.sudo(f"mkdir -p {quote(env.project_path)}")
        conn.sudo(f"mkdir -p {quote(env.go_path + '/bin')}")
        conn.sudo(f"mkdir -p {quote(str(Path(env.code_root).parent))}")
        conn.sudo(f"chown {env.runner_user}:{env.runner_user} {quote(env.go_path)} -R")

    if not _exists(db_path):
        conn.sudo(f"mkdir -p {quote(db_path)}")
        conn.sudo(f"chown {env.runner_user}:{env.runner_group} {quote(db_path)}")

    _upload_template(
        "conf/ffdb.service", "/etc/systemd/system/ffdb.service", _template_context()
    )
    _update_and_build(("go get .", "go install"))
    conn.sudo(
        f"chown {env.runner_user}:{env.runner_group} {quote(env.go_path + '/bin/ffdb')}"
    )
    conn.sudo("systemctl daemon-reload")
    conn.sudo("systemctl enable ffdb.service")
    conn.sudo("systemctl restart ffdb.service")


@task(name="deploy_client")
def deploy_client(_ctx):
    conn = _conn()
    db_path = env.httpcache_path

    if not _exists(env.code_root):
        conn.sudo(f"mkdir -p {quote(env.project_path)}")
        conn.sudo(f"mkdir -p {quote(env.go_path + '/bin')}")
        conn.sudo(f"mkdir -p {quote(str(Path(env.code_root).parent))}")
        conn.sudo(f"mkdir -p {quote(str(Path(env.ffclient_logfile).parent))}")
        conn.sudo(f"chown {env.runner_user}:{env.runner_user} {quote(env.go_path)} -R")

    if not _exists(db_path):
        conn.sudo(f"mkdir -p {quote(db_path)}")
        conn.sudo(f"chown {env.runner_user}:{env.runner_group} {quote(db_path)}")

    log_dir = str(Path(env.ffclient_logfile).parent)
    conn.sudo(f"chown {env.runner_user} {quote(log_dir)}")
    conn.sudo(f"chmod -R 775 {quote(log_dir)}")
    _upload_template("conf/ffclient.conf", "/etc/init/ffclient.conf", _template_context())

    _update_and_build(
        (
            "cd client && go get .",
            f"cd client && go build && mv client {quote(env.go_path + '/bin/ffclient')}",
        )
    )
    conn.sudo("stop ffclient", warn=True)
    conn.sudo("start ffclient")


@task(name="deploy_web")
def deploy_web(_ctx):
    conn = _conn()
    web_path = f"{env.project_path}/www"
    if not _exists(web_path):
        conn.sudo(f"mkdir -p {quote(web_path)}")
        conn.sudo(f"chown {env.runner_user}:{env.runner_group} {quote(web_path)}")

    key_path = f"{env.project_path}/gauth.json"
    _upload_template("conf/gauth.json", key_path, _template_context())
    conn.sudo(f"chown {env.runner_user}:{env.runner_group} {quote(key_path)}")
    conn.sudo(f"chmod 600 {quote(key_path)}")

    context = _template_context(
        salt=Path("conf/salt.conf").read_text(encoding="utf-8").strip(),
        config_file=f"{env.project_path}/config.json",
        web_path=web_path,
        www_public_path=web_path,
    )
    _upload_template("conf/ffweb.service", "/etc/systemd/system/ffweb.service", context)

    _update_and_build(
        (
            "cd httpd/app && corepack enable pnpm && pnpm install --frozen-lockfile && pnpm run build",
            "cd httpd && go get .",
            "cd httpd && go build",
        ),
        clean=True,
    )

    web_bin_path = f"{env.go_path}/bin/ffweb"
    conn.sudo(f"mv {quote(env.code_root + '/httpd/httpd')} {quote(web_bin_path)}")
    conn.sudo(f"chown {env.runner_user}:{env.runner_group} {quote(web_bin_path)} -R")
    conn.sudo("systemctl daemon-reload")
    conn.sudo("systemctl enable ffweb.service")
    conn.sudo("systemctl restart ffweb.service")


@task(name="deploy_ssl")
def deploy_ssl(_ctx):
    """Create the production SSL key and certificate request."""
    conn = _conn()
    domain = env.nginx_server_name
    ssl_path = "/srv/ssl"
    conn.sudo(f"mkdir -p {quote(ssl_path)}")

    key_file = f"{ssl_path}/{domain}.key"
    csr_file = f"{ssl_path}/{domain}.csr"
    crt_file = f"{ssl_path}/{domain}.crt"
    if not _exists(csr_file):
        conn.sudo(
            f"openssl req -nodes -newkey rsa:2048 -keyout {quote(key_file)} "
            f"-out {quote(csr_file)}"
        )
        raise Exit("put keys in dir when run this again")

    conn.sudo(f"chmod 400 {quote(key_file)}")
    conn.sudo(f"chmod 400 {quote(crt_file)}")


@task(name="deploy_nginx")
def deploy_nginx(_ctx):
    """Deploy the HTTP nginx origin configuration."""
    conn = _conn()
    web_path = f"{env.project_path}/www"
    nginx_conf_file = "/etc/nginx/sites-enabled/friendfeed.conf"
    _upload_template(
        "conf/nginx_http.conf",
        nginx_conf_file,
        _template_context(www_public_path=web_path),
    )

    result = conn.sudo(
        "nginx -t -c /etc/nginx/nginx.conf", hide=True, warn=True
    )
    if result.failed:
        raise Exit("NGINX configuration test failed; configuration was not reloaded")
    conn.sudo("nginx -s reload")
