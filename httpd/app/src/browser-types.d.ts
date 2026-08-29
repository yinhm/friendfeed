export type FeedRefView = {
  id: string; name: string; picture?: string; private?: boolean;
};
export type EntryView = {
  id: string; from: FeedRefView; to?: FeedRefView[]; title?: string;
  body: string; rawBody?: string; type?: string; date?: string;
  via?: {name: string; url: string}; commands: string[];
  thumbnails?: Array<{width?: number; height?: number; link: string; url: string;
    video?: {provider: 'youtube'; id: string}}>;
  files?: Array<{url: string; name: string; type?: string; size?: number}>;
  comments?: Array<{id?: string; body: string; rawBody?: string; date?: string;
    placeholder?: boolean; commands?: string[]; from?: FeedRefView}>;
  likes?: Array<{placeholder?: boolean; body?: string; from: FeedRefView}>;
};
export type FeedView = {
  id: string; uuid: string; name?: string; picture?: string;
  description?: string; private?: boolean; commands?: string[]; entries: EntryView[];
};
export type FeedPageData = {
  feed: FeedView; show_header: boolean; show_paging: boolean; show_share: boolean;
  show_profile_relations: boolean;
  show_prev: boolean; show_next: boolean; prev_start: number; next_start: number;
  cursor_paging: boolean; next_cursor: string; realtime_enabled: boolean;
  realtime_home: boolean; onpage: boolean; onpage_edit: boolean; query: string;
  manage_services_url?: string; group_settings_url?: string; group_members_url?: string;
};
export type ProfileSummary = {uuid: string; id: string; name: string; picture?: string};
export type PageBootstrap<T = unknown> = {
  version: 1; page: string; current_user?: ProfileSummary;
  layout?: {onpage: boolean; has_unread_notifications: boolean; show_groups: boolean;
    groups?: Array<{id: string; name: string; private?: boolean}>; archive_feed_id?: string;
    archive_years?: Array<{year: number; count: number; cursor?: string}>};
  data: T;
};
export type AccountPageData = {
  tab: 'profile' | 'import';
  profile: {uuid: string; id: string; name: string; description?: string;
    picture?: string; private?: boolean; type?: string};
  services: Record<string, {id: string; name: string; icon?: string; profile?: string;
    username?: string; kind?: string; service_uuid?: string; enabled?: boolean}>;
  states: Record<string, {last_fetch_ms?: number; next_fetch_ms?: number;
    last_error?: string; status?: string; last_success_ms?: number}>;
  target: string;
};
export type FeedImportPageData = {
  feed: {id: string; name: string; type: string};
  services: AccountPageData['services']; states: AccountPageData['states']; target: string;
  manage_services_url: string; group_settings_url?: string; group_members_url?: string;
};
export type NotificationsPageData = {
  items: Array<{text: string; href: string; date: string}>; next_cursor?: string;
};
export type RequestsPageData = {
  requests: Array<{requester: AccountPageData['profile']; requested_at: string}>;
  private: boolean; error?: string;
};
export type GroupCreatePageData = {
  group: {id: string; name: string; description?: string; picture?: string; private?: boolean};
  error?: string;
};
export type GroupSettingsPageData = {
  group: GroupCreatePageData['group']; error?: string;
};
export type GroupMembersPageData = {
  group: GroupCreatePageData['group'];
  members: Array<{profile: AccountPageData['profile']; is_admin: boolean}>;
  requests: RequestsPageData['requests']; has_more: boolean; can_manage: boolean; error?: string;
};
export type GroupsPageData = {
  heading: string; groups: GroupCreatePageData['group'][];
  page: 'discover' | 'mine'; empty_text: string; next_cursor?: string;
};
export type ProfileRelationsPageData = {
  profile: {id: string; name: string; picture?: string; private?: boolean};
  relation: 'following' | 'followers';
  profiles: Array<{id: string; name: string; picture?: string; private?: boolean}>;
};
