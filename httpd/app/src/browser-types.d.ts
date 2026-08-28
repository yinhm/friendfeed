export type FeedRefView = {
  id: string; name: string; picture?: string; private?: boolean;
};
export type EntryView = {
  id: string; from: FeedRefView; to?: FeedRefView[]; title?: string;
  body: string; rawBody?: string; type?: string; date?: string;
  via?: {name: string; url: string}; commands: string[];
  thumbnails?: Array<{width?: number; height?: number; link: string; url: string}>;
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
  show_prev: boolean; show_next: boolean; prev_start: number; next_start: number;
  cursor_paging: boolean; next_cursor: string; realtime_enabled: boolean;
  realtime_home: boolean; onpage: boolean; onpage_edit: boolean; query: string;
  manage_services_url?: string; group_settings_url?: string; group_members_url?: string;
};
export type ProfileSummary = {uuid: string; id: string; name: string; picture?: string};
export type PageBootstrap<T = unknown> = {
  version: 1; page: string; current_user?: ProfileSummary; data: T;
};
