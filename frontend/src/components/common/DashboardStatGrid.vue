<template>
  <!-- Equal fractional columns: every stat grid on a page divides the same width the same way, so the shared
       one-pixel lines of stacked grids fall on the same x positions. -->
  <div class="ag-stat-grid grid grid-cols-2 gap-4 lg:grid-cols-4"><slot /></div>
</template>
