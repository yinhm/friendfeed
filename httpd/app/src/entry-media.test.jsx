import {render, screen, fireEvent} from '@testing-library/react';

import {EntryMediaBox} from './entry';

const thumbs = [
  {url: '/media/t1.jpg', link: '/media/o1.jpg', width: 1024, height: 683},
  {url: '/media/t2.jpg', link: '/media/o2.jpg', width: 800, height: 800},
];

test('media box keeps intrinsic dimensions as attributes for proportional scaling', () => {
  const {container} = render(<EntryMediaBox thumbs={thumbs} />);

  const imgs = container.querySelectorAll('.media img');
  expect(imgs).toHaveLength(2);
  expect(imgs[0]).toHaveAttribute('width', '1024');
  expect(imgs[0]).toHaveAttribute('height', '683');
  expect(container.querySelector('.media')).toHaveAttribute('data-count', '2');
});

test('clicking a media image opens an in-page lightbox with the original', () => {
  render(<EntryMediaBox thumbs={thumbs} />);

  const triggers = screen.getAllByRole('button', {name: 'Open media'});
  expect(triggers[1]).not.toHaveAttribute('href');
  fireEvent.click(triggers[1]);

  const dialog = screen.getByRole('dialog', {name: 'Enlarged media'});
  expect(dialog.querySelector('img')).toHaveAttribute('src', '/media/o2.jpg');
  expect(document.querySelector('.media a')).toBeNull();
});

test('lightbox falls back to the thumbnail without creating a media link', () => {
  render(<EntryMediaBox thumbs={[{url: '/media/only.jpg', link: ''}]} />);

  fireEvent.click(screen.getByRole('button', {name: 'Open media'}));
  expect(screen.getByRole('dialog', {name: 'Enlarged media'}).querySelector('img'))
    .toHaveAttribute('src', '/media/only.jpg');
  expect(document.querySelector('.media a')).toBeNull();
});

test('lightbox ignores an HTML page link and shows the thumbnail immediately', () => {
  render(<EntryMediaBox thumbs={[{
    url: 'https://pbs.twimg.com/media/photo.jpg',
    link: 'https://twitter.com/user/status/1/photo/1',
  }]} />);

  fireEvent.click(screen.getByRole('button', {name: 'Open media'}));
  const image = screen.getByRole('dialog', {name: 'Enlarged media'}).querySelector('img');
  expect(image).toHaveAttribute('src', 'https://pbs.twimg.com/media/photo.jpg');
});

test('lightbox falls back if a direct original image fails to load', () => {
  render(<EntryMediaBox thumbs={[{url: '/media/thumb.jpg', link: '/media/original.jpg'}]} />);

  fireEvent.click(screen.getByRole('button', {name: 'Open media'}));
  const image = screen.getByRole('dialog', {name: 'Enlarged media'}).querySelector('img');
  expect(image).toHaveAttribute('src', '/media/original.jpg');
  fireEvent.error(image);
  expect(image).toHaveAttribute('src', '/media/thumb.jpg');
});

test('lightbox closes on click and on Escape', () => {
  render(<EntryMediaBox thumbs={thumbs} />);

  fireEvent.click(screen.getAllByRole('button', {name: 'Open media'})[0]);
  fireEvent.click(screen.getByRole('dialog', {name: 'Enlarged media'}));
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();

  fireEvent.click(screen.getAllByRole('button', {name: 'Open media'})[0]);
  fireEvent.keyDown(document, {key: 'Escape'});
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});

test('lightbox navigates with arrow keys and buttons, wrapping around', () => {
  render(<EntryMediaBox thumbs={thumbs} />);

  fireEvent.click(screen.getAllByRole('button', {name: 'Open media'})[0]);
  const dialog = screen.getByRole('dialog', {name: 'Enlarged media'});
  expect(dialog.querySelector('img')).toHaveAttribute('src', '/media/o1.jpg');

  fireEvent.keyDown(document, {key: 'ArrowRight'});
  expect(dialog.querySelector('img')).toHaveAttribute('src', '/media/o2.jpg');

  // Wraps past the last image back to the first.
  fireEvent.keyDown(document, {key: 'ArrowRight'});
  expect(dialog.querySelector('img')).toHaveAttribute('src', '/media/o1.jpg');

  fireEvent.keyDown(document, {key: 'ArrowLeft'});
  expect(dialog.querySelector('img')).toHaveAttribute('src', '/media/o2.jpg');

  // Nav buttons navigate without closing the lightbox.
  fireEvent.click(screen.getByRole('button', {name: 'Previous media'}));
  expect(dialog.querySelector('img')).toHaveAttribute('src', '/media/o1.jpg');
  fireEvent.click(screen.getByRole('button', {name: 'Next media'}));
  expect(dialog.querySelector('img')).toHaveAttribute('src', '/media/o2.jpg');
  expect(screen.getByRole('dialog')).toBeInTheDocument();
});

test('a single media image shows no navigation buttons', () => {
  render(<EntryMediaBox thumbs={[thumbs[0]]} />);

  fireEvent.click(screen.getByRole('button', {name: 'Open media'}));
  expect(screen.getByRole('dialog', {name: 'Enlarged media'})).toBeInTheDocument();
  expect(screen.queryByRole('button', {name: 'Next media'})).not.toBeInTheDocument();
});

test('a validated YouTube thumbnail uses a click-to-load privacy facade', async () => {
  const {container} = render(<EntryMediaBox thumbs={[{
    url: 'http://img.youtube.com/vi/nJDf-sdylwU/2.jpg',
    link: 'https://twitter.com/user/status/1',
    video: {provider: 'youtube', id: 'nJDf-sdylwU'},
  }]} />);

  const play = await screen.findByRole('button', {name: 'Watch YouTube video'});
  expect(container.querySelector('.ff-youtube')).toBeInTheDocument();
  expect(screen.queryByRole('button', {name: 'Open media'})).not.toBeInTheDocument();
  fireEvent.click(play);
  expect(await screen.findByTitle('YouTube video')).toHaveAttribute(
    'src', expect.stringContaining('https://www.youtube-nocookie.com/embed/nJDf-sdylwU')
  );
});
