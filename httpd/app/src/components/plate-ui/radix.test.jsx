import {fireEvent, render, screen, waitFor} from '@testing-library/react';

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from './dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from './dropdown-menu';
import {Popover, PopoverContent, PopoverTrigger} from './popover';

test('dialog opens in a portal, traps focus, and closes with Escape', async () => {
  render(
    <Dialog>
      <DialogTrigger>Open dialog</DialogTrigger>
      <DialogContent>
        <DialogTitle>Settings</DialogTitle>
        <DialogDescription>Dialog content</DialogDescription>
        <button>Focusable action</button>
      </DialogContent>
    </Dialog>
  );

  fireEvent.click(screen.getByRole('button', {name: 'Open dialog'}));

  const dialog = await screen.findByRole('dialog', {name: 'Settings'});
  expect(dialog.parentElement).toBe(document.body);
  await waitFor(() => expect(dialog).toContainElement(document.activeElement));

  fireEvent.keyDown(document.activeElement, {key: 'Escape'});
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
});

test('popover opens in a portal and closes with Escape', async () => {
  render(
    <Popover>
      <PopoverTrigger>Open popover</PopoverTrigger>
      <PopoverContent>Popover content</PopoverContent>
    </Popover>
  );

  fireEvent.click(screen.getByRole('button', {name: 'Open popover'}));

  const content = await screen.findByText('Popover content');
  expect(content.parentElement?.parentElement).toBe(document.body);
  expect(content).toHaveStyle({zIndex: '1000'});

  fireEvent.keyDown(document.activeElement, {key: 'Escape'});
  await waitFor(() => expect(screen.queryByText('Popover content')).not.toBeInTheDocument());
});

test('dropdown menu supports keyboard navigation', async () => {
  render(
    <DropdownMenu>
      <DropdownMenuTrigger>Open menu</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem>First action</DropdownMenuItem>
        <DropdownMenuItem>Second action</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );

  const trigger = screen.getByRole('button', {name: 'Open menu'});
  trigger.focus();
  fireEvent.keyDown(trigger, {key: 'ArrowDown'});

  const menu = await screen.findByRole('menu');
  expect(menu.parentElement?.parentElement).toBe(document.body);
  await waitFor(() => expect(screen.getByRole('menuitem', {name: 'First action'})).toHaveFocus());

  fireEvent.keyDown(document.activeElement, {key: 'ArrowDown'});
  await waitFor(() => expect(screen.getByRole('menuitem', {name: 'Second action'})).toHaveFocus());
});
