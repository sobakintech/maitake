<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api } from '#lib/api.js';
	import logo from '#lib/assets/logo.png';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Sidebar from '#lib/components/ui/sidebar/index.js';
	import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
	import LogOutIcon from '@lucide/svelte/icons/log-out';
	import MoonIcon from '@lucide/svelte/icons/moon';
	import SunIcon from '@lucide/svelte/icons/sun';
	import UsersIcon from '@lucide/svelte/icons/users';
	import LayoutDashboardIcon from '@lucide/svelte/icons/layout-dashboard';
	import ServerIcon from '@lucide/svelte/icons/server';
	import ChartColumnIcon from '@lucide/svelte/icons/chart-column';
	import GaugeIcon from '@lucide/svelte/icons/gauge';
	import SettingsIcon from '@lucide/svelte/icons/settings';
	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import BoxesIcon from '@lucide/svelte/icons/boxes';
	import CalendarClockIcon from '@lucide/svelte/icons/calendar-clock';
	import ScrollTextIcon from '@lucide/svelte/icons/scroll-text';
	import CircleArrowUpIcon from '@lucide/svelte/icons/circle-arrow-up';
	import AlarmClockIcon from '@lucide/svelte/icons/alarm-clock';
	import SlidersHorizontalIcon from '@lucide/svelte/icons/sliders-horizontal';
	import { mode, toggleMode } from 'mode-watcher';
	import type { LayoutProps } from './$types';

	let { data, children }: LayoutProps = $props();

	const nav = [
		{
			label: 'Manage',
			items: [
				{ href: '/overview', label: 'Overview', icon: LayoutDashboardIcon },
				{ href: '/accounts', label: 'Accounts', icon: ServerIcon },
				{ href: '/users', label: 'Users', icon: UsersIcon },
				{ href: '/keys', label: 'API keys', icon: KeyRoundIcon },
				{ href: '/models', label: 'Models', icon: BoxesIcon }
			]
		},
		{
			label: 'Activity',
			items: [
				{ href: '/timeline', label: 'Timeline', icon: CalendarClockIcon },
				{ href: '/usage', label: 'Usage', icon: ChartColumnIcon },
				{ href: '/audit', label: 'Audit log', icon: ScrollTextIcon }
			]
		},
		{
			label: 'Configure',
			items: [
				{ href: '/pool', label: 'Account pool', icon: SlidersHorizontalIcon },
				{ href: '/primer', label: 'Session primer', icon: AlarmClockIcon },
				{ href: '/quota-page', label: 'Quota page', icon: GaugeIcon },
				{ href: '/settings', label: 'Settings', icon: SettingsIcon }
			]
		}
	];

	async function logout() {
		await api.logout();
		await goto('/login', { invalidateAll: true });
	}
	import PrivacyToggle from '#lib/components/privacy-toggle.svelte';
</script>

<Sidebar.Provider>
	<Sidebar.Root collapsible="icon">
		<Sidebar.Header>
			<Sidebar.Menu>
				<Sidebar.MenuItem>
					<Sidebar.MenuButton size="lg">
						{#snippet child({ props })}
							<a href="/overview" {...props}>
								<img src={logo} alt="" class="size-8 shrink-0" />
								<span class="font-semibold">maitake</span>
							</a>
						{/snippet}
					</Sidebar.MenuButton>
				</Sidebar.MenuItem>
			</Sidebar.Menu>
		</Sidebar.Header>
		<Sidebar.Content>
			{#each nav as group (group.label)}
				<Sidebar.Group>
					<Sidebar.GroupLabel>{group.label}</Sidebar.GroupLabel>
					<Sidebar.Menu>
						{#each group.items as item (item.href)}
							<Sidebar.MenuItem>
								<Sidebar.MenuButton
									isActive={page.url.pathname.startsWith(item.href)}
									tooltipContent={item.label}
								>
									{#snippet child({ props })}
										<a href={item.href} {...props}>
											<item.icon />
											<span>{item.label}</span>
										</a>
									{/snippet}
								</Sidebar.MenuButton>
							</Sidebar.MenuItem>
						{/each}
					</Sidebar.Menu>
				</Sidebar.Group>
			{/each}
		</Sidebar.Content>
		<Sidebar.Footer>
			<Sidebar.Menu>
				{#if data.update}
					<Sidebar.MenuItem>
						<Sidebar.MenuButton tooltipContent="Update available">
							{#snippet child({ props })}
								<a href="/settings?tab=about" {...props}>
									<CircleArrowUpIcon class="text-primary" />
									<span>Update available</span>
									<span class="ml-auto font-mono text-xs text-muted-foreground"
										>{data.update?.version}</span
									>
								</a>
							{/snippet}
						</Sidebar.MenuButton>
					</Sidebar.MenuItem>
				{/if}
				<Sidebar.MenuItem>
					<DropdownMenu.Root>
						<DropdownMenu.Trigger>
							{#snippet child({ props })}
								<Sidebar.MenuButton size="lg" {...props}>
									<div
										class="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-sm font-medium uppercase"
									>
										{data.admin.username.slice(0, 1)}
									</div>
									<div class="grid flex-1 text-left leading-tight">
										<span class="truncate font-medium">{data.admin.username}</span>
										<span class="truncate text-xs text-muted-foreground">Admin</span>
									</div>
									<ChevronsUpDownIcon class="ml-auto" />
								</Sidebar.MenuButton>
							{/snippet}
						</DropdownMenu.Trigger>
						<DropdownMenu.Content side="top" align="start" class="w-56">
							<DropdownMenu.Item onSelect={toggleMode}>
								{#if mode.current === 'dark'}<SunIcon /> Light mode{:else}<MoonIcon /> Dark mode{/if}
							</DropdownMenu.Item>
							<DropdownMenu.Separator />
							<DropdownMenu.Item onSelect={logout}>
								<LogOutIcon /> Sign out
							</DropdownMenu.Item>
							{#if data.version}
								<DropdownMenu.Separator />
								<DropdownMenu.Label class="font-normal">maitake {data.version}</DropdownMenu.Label>
							{/if}
						</DropdownMenu.Content>
					</DropdownMenu.Root>
				</Sidebar.MenuItem>
			</Sidebar.Menu>
		</Sidebar.Footer>
		<Sidebar.Rail />
	</Sidebar.Root>
	<Sidebar.Inset>
		<header class="flex h-12 shrink-0 items-center gap-2 border-b px-4">
			<Sidebar.Trigger class="-ml-1" />
			<div class="ml-auto"><PrivacyToggle /></div>
		</header>
		<div class="flex-1 p-6">
			{@render children()}
		</div>
	</Sidebar.Inset>
</Sidebar.Provider>
