#!/usr/bin/env python
#
# Copyright 2009 FriendFeed
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may
# not use this file except in compliance with the License. You may obtain
# a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
# WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
# License for the specific language governing permissions and limitations
# under the License.

"""A Python implementation of the FriendFeed API v2

Documentation is available at http://friendfeed.com/api/documentation.
For a complete example application using this library, see
http://code.google.com/p/friendfeed-api-example/.

For version 1 of the API, see
http://code.google.com/p/friendfeed-api/wiki/ApiDocumentation.
"""

import base64
import binascii
import cgi
import datetime
import functools
import hashlib
import hmac
import time
import urllib
import urllib2
import urlparse
import uuid

# Find a JSON parser
try:
    import simplejson
    _parse_json = lambda s: simplejson.loads(s.decode("utf-8"))
except ImportError:
    try:
        import cjson
        _parse_json = lambda s: cjson.decode(s.decode("utf-8"), True)
    except ImportError:
        try:
            import json
            _parse_json = lambda s: _unicodify(json.read(s))
        except ImportError:
            # For Google AppEngine
            from django.utils import simplejson
            _parse_json = lambda s: simplejson.loads(s.decode("utf-8"))

_FRIENDFEED_API_BASE = "http://friendfeed-api.com/v2"


def _authenticated(method):
    @functools.wraps(method)
    def wrapper(self, *args, **kwargs):
        if not self.auth_nickname or not self.auth_key:
            raise Exception("Remote key required for this method")
        return method(self, *args, **kwargs)
    return wrapper


class FriendFeed(object):
    # def __init__(self, oauth_consumer_token=None, oauth_access_token=None):
    #     """Initializes a FriendFeed session.

    #     To make authenticated requests to FriendFeed, which is required for
    #     some feeds and to post messages, you must provide both
    #     oauth_consumer_token and oauth_access_token. They should both be
    #     dictionaries of the form {"key": "...", "secret": "..."}. Learn
    #     more about OAuth at http://friendfeed.com/api/oauth.

    #     You can register your application to receive your FriendFeed OAuth
    #     Consumer Key at http://friendfeed.com/api/register. To fetch request
    #     tokens and access tokens, see fetch_oauth_request_token and
    #     fetch_oauth_access_token below.
    #     """
    #     self.consumer_token = oauth_consumer_token
    #     self.access_token = oauth_access_token

    def __init__(self, auth_nickname=None, auth_key=None):
        """Creates a new FriendFeed session for the given user.

        The credentials are optional for some operations, but required for
        private feeds and all operations that write data, like publish_link.
        """
        self.auth_nickname = auth_nickname
        self.auth_key = auth_key

    def fetch_feed(self, feed_id, **args):
        """Fetches the feed with the given ID, e.g., "bret" or "home"

        See http://friendfeed.com/api/documentation#read_feed.
        The feed is authenticated/personalized if the OAuth parameters are
        set for this session.
        """
        return self.fetch("/feed/" + feed_id, **args)

    def fetch_search_feed(self, q, **args):
        """Fetches the search results for the given query.

        See http://friendfeed.com/api/documentation#read_search.
        """
        return self.fetch("/search", q=q, **args)

    @_authenticated
    def fetch_feed_list(self, **args):
        """Fetches the feed menu for the authenticated user's FriendFeed.

        See http://friendfeed.com/api/documentation#read_feedlist.
        Authentication is required for this method.
        """
        return self.fetch("/feedlist", **args)

    def fetch_feed_info(self, feed_id, **args):
        """Fetches the meta data about the feed with the given ID.

        See http://friendfeed.com/api/documentation#read_feedinfo.
        """
        return self.fetch("/feedinfo/" + feed_id, **args)

    def fetch_entry(self, entry_id, **args):
        """Fetches the entry with the given ID.

        See http://friendfeed.com/api/documentation#read_entry.
        """
        return self.fetch("/entry/" + entry_id, **args)

    def fetch_comment(self, comment_id, **args):
        """Fetches the comment with the given ID.

        See http://friendfeed.com/api/documentation#read_comment.
        """
        return self.fetch("/comment/" + comment_id, **args)

    def fetch_url_feed(self, url, **args):
        """Fetches the entries that link to the given URL.

        See http://friendfeed.com/api/documentation#read_url.
        """
        return self.fetch("/url", url=url, **args)

    def fetch_host_feed(self, host, **args):
        """Fetches the entries with links from the given host.

        See http://friendfeed.com/api/documentation#read_url.
        """
        return self.fetch("/url", host=host, **args)

    @_authenticated
    def post_entry(self, body, link=None, to=None, **args):
        """Posts the given message to FriendFeed (link and to optional).

        See http://friendfeed.com/api/documentation#write_entry.
        Authentication is required for this method.
        """
        args.update(body=body)
        if link: args.update(link=link)
        if to: args.update(to=to)
        return self.fetch("/entry", post_args=args)

    @_authenticated
    def edit_entry(self, id, body=None, link=None, **args):
        """Edits the given properties on the entry with the given ID.

        See http://friendfeed.com/api/documentation#write_entry.
        Authentication is required for this method.
        """
        args.update(id=id)
        if body: args.update(body=body)
        if link: args.update(link=link)
        return self.fetch("/entry", post_args=args)

    @_authenticated
    def delete_entry(self, id, **args):
        """Deletes the given entry from FriendFeed.

        See http://friendfeed.com/api/documentation#write_entry.
        Authentication is required for this method.
        """
        args.update(id=id)
        return self.fetch("/entry/delete", post_args=args)

    @_authenticated
    def post_comment(self, entry, body, **args):
        """Posts the given comment to FriendFeed.

        See http://friendfeed.com/api/documentation#write_comment.
        Authentication is required for this method.
        """
        args.update(entry=entry, body=body)
        return self.fetch("/comment", post_args=args)

    @_authenticated
    def edit_comment(self, id, body, **args):
        """Edits the given properties on the comment with the given ID.

        See http://friendfeed.com/api/documentation#write_comment.
        Authentication is required for this method.
        """
        args.update(id=id, body=body)
        return self.fetch("/comment", post_args=args)

    @_authenticated
    def delete_comment(self, id, **args):
        """Deletes the given comment from FriendFeed.

        See http://friendfeed.com/api/documentation#write_comment.
        Authentication is required for this method.
        """ 
        args.update(id=id)
        return self.fetch("/comment/delete", post_args=args)

    @_authenticated
    def post_like(self, entry, **args):
        """Posts the given like to FriendFeed.

        See http://friendfeed.com/api/documentation#write_like.
        Authentication is required for this method.
        """
        args.update(entry=entry)
        return self.fetch("/like", post_args=args)

    @_authenticated
    def delete_like(self, entry, **args):
        """Deletes the given like from FriendFeed.

        See http://friendfeed.com/api/documentation#write_like.
        Authentication is required for this method.
        """ 
        args.update(entry=entry)
        return self.fetch("/like/delete", post_args=args)

    @_authenticated
    def hide_entry(self, entry, **args):
        """Hides the given entry from the authenticated user's FriendFeed.

        See http://friendfeed.com/api/documentation#write_hide.
        Authentication is required for this method.
        """
        args.update(entry=entry)
        return self.fetch("/hide", post_args=args)

    @_authenticated
    def unhide_entry(self, entry, **args):
        """Un-hides the given entry from the authenticated user's FriendFeed.

        See http://friendfeed.com/api/documentation#write_hide.
        Authentication is required for this method.
        """
        return self.hide_entry(entry, unhide=1, **args)

    @_authenticated
    def subscribe(self, feed, **args):
        """Subscribes the authenticated user to the given feed.

        See http://friendfeed.com/api/documentation#write_subscribe.
        Authentication is required for this method.
        """
        args.update(feed=feed)
        return self.fetch("/subscribe", post_args=args)

    @_authenticated
    def unsubscribe(self, feed, **args):
        """Unsubscribes the authenticated user from the given feed.

        See http://friendfeed.com/api/documentation#write_unsubscribe.
        Authentication is required for this method.
        """
        args.update(feed=feed)
        return self.fetch("/unsubscribe", post_args=args)

    @_authenticated
    def edit_feed_info(self, feed=None, name=None, description=None, **args):
        """Updates the name and/or description of the given feed.

        If feed_id is not specified, we update the profile of the
        authenticated user.
        See http://friendfeed.com/api/documentation#write_feedinfo.
        """
        if feed: args.update(feed=feed)
        if name: args.update(name=name)
        if description: args.update(description=description)
        return self.fetch("/feedinfo", post_args=args)

    def fetch(self, path, post_args=None, **args):
        """Fetches the given relative API path, e.g., "/bret/friends"

        If the request is a POST, post_args should be provided. Query
        string arguments should be given as keyword arguments.
        """
        url = _FRIENDFEED_API_BASE + path

        if args: url += "?" + urllib.urlencode(args)
        if post_args is not None:
            request = urllib2.Request(url, urllib.urlencode(post_args))
        else:
            request = urllib2.Request(url)

        if self.auth_nickname and self.auth_key:
            pair = "%s:%s" % (self.auth_nickname, self.auth_key)
            token = base64.b64encode(pair)
            request.add_header("Authorization", "Basic %s" % token)

        stream = urllib2.urlopen(request)
        data = stream.read()
        stream.close()
        f = open("dump.json", "w")
        f.write(data)
        f.close()
        return self._parse_dates(_parse_json(data))

    def _parse_dates(self, obj):
        if isinstance(obj, dict):
            for name in obj.keys():
                if name == u"date":
                    obj[name] = datetime.datetime.strptime(
                        obj[name], "%Y-%m-%dT%H:%M:%SZ")
                else:
                    self._parse_dates(obj[name])
        elif isinstance(obj, list):
            for subobj in obj:
                self._parse_dates(subobj)
        return obj


def _example():
    # Fill in a nickname and a valid remote key below for authenticated
    # actions like posting an entry and reading a protected feed
    # session = FriendFeed(auth_nickname=nickname, auth_key=remote_key)
    session = FriendFeed(auth_nickname='yinhm', auth_key='fruit696mayo')

    # feed = session.fetch_feed("public", locale="zh-cn")
    feed = session.fetch_feed("home")
    for entry in feed["entries"]:
        for k, v in entry.iteritems():
            print k, v
        break

    # if session.auth_nickname and session.auth_key:
    #     # The feed that the authenticated user would see on their home page
    #     feed = session.fetch_feed("home")
    #     print feed

if __name__ == "__main__":
    _example()
