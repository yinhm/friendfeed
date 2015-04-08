#!/usr/bin/env python
# -*- coding: utf-8 -*-
#
# Copyright 2015 yinhm

import api1
import api2


ff1 = api1.FriendFeed(auth_nickname='yinhm', auth_key='fruit696mayo')
ff2 = api2.FriendFeed(auth_nickname='yinhm', auth_key='fruit696mayo')


# profile = ff1.fetch_user_profile('yinhm')

# http://friendfeed-api.com/v2/feed/yinhm?num=100&raw=1&start=500
ff2.fetch_feed('yinhm', num=100, start=500, raw=1)

