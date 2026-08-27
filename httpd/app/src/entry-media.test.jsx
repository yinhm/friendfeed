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

  fireEvent.click(screen.getAllByRole('link', {name: 'Open media'})[1]);

  const dialog = screen.getByRole('dialog', {name: 'Enlarged media'});
  expect(dialog.querySelector('img')).toHaveAttribute('src', '/media/o2.jpg');
  // The click must not navigate to the original URL.
  expect(window.location.pathname).toBe('/');
});

test('lightbox closes on click and on Escape', () => {
  render(<EntryMediaBox thumbs={thumbs} />);

  fireEvent.click(screen.getAllByRole('link', {name: 'Open media'})[0]);
  fireEvent.click(screen.getByRole('dialog', {name: 'Enlarged media'}));
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();

  fireEvent.click(screen.getAllByRole('link', {name: 'Open media'})[0]);
  fireEvent.keyDown(document, {key: 'Escape'});
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});

test('lightbox navigates with arrow keys and buttons, wrapping around', () => {
  render(<EntryMediaBox thumbs={thumbs} />);

  fireEvent.click(screen.getAllByRole('link', {name: 'Open media'})[0]);
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

  fireEvent.click(screen.getByRole('link', {name: 'Open media'}));
  expect(screen.getByRole('dialog', {name: 'Enlarged media'})).toBeInTheDocument();
  expect(screen.queryByRole('button', {name: 'Next media'})).not.toBeInTheDocument();
});
