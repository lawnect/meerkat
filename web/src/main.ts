import { mount } from 'svelte';
import App from './App.svelte';
import { i18n } from './i18n.svelte';
import { applyTheme } from './theme.svelte';
import './app.css';

const target = document.getElementById('app');
if (!target) throw new Error('App mount element #app was not found');

document.documentElement.lang = i18n.locale;
applyTheme();
mount(App, { target });
