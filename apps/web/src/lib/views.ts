import type { IconName } from './components/Icon.svelte';

/** The dashboard's views. The header switch renders VIEW_OPTIONS in order,
 * so a new view is one more entry here plus its branch in +page.svelte; every
 * view gets the same filtered monitors, environment, service selection and
 * period (the chart has its own time range instead of the period). */
export type View = 'list' | 'board' | 'chart';

export interface ViewOption {
	id: View;
	label: string;
	icon: IconName;
}

export const VIEW_OPTIONS: ViewOption[] = [
	{ id: 'list', label: 'Lista', icon: 'list-check' },
	{ id: 'board', label: 'Board', icon: 'layout-masonry-line' },
	{ id: 'chart', label: 'Gráfico', icon: 'line-chart-line' }
];

export function isView(value: unknown): value is View {
	return VIEW_OPTIONS.some((option) => option.id === value);
}
