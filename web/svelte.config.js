export default {
  compilerOptions: {
    warningFilter: (warning) => warning.code !== 'a11y_no_noninteractive_tabindex',
  },
};
