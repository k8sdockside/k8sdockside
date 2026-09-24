<!-- For WhenVisible's test: something far down a scrolling box, under WhenVisible. -->
<script lang="ts">
    import WhenVisible from '../WhenVisible.svelte';
</script>

<div class="box" style="height: 300px; overflow: auto;">
    <div style="height: 3000px;"></div>
    <WhenVisible height={100} margin="0px">
        <p class="late">drawn</p>
    </WhenVisible>
</div>
