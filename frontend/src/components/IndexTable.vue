<template>
  <BBTable
    :column-list="columnList"
    :section-data-source="sectionList"
    :show-header="true"
    :compact-section="false"
  >
    <template #header>
      <BBTableHeaderCell
        :left-padding="4"
        class="w-16"
        :title="columnList[0].title"
      />
      <BBTableHeaderCell class="w-4" :title="columnList[1].title" />
      <BBTableHeaderCell class="w-4" :title="columnList[2].title" />
      <BBTableHeaderCell
        v-if="showVisibleColumn"
        class="w-4"
        :title="columnList[3].title"
      />
      <BBTableHeaderCell class="w-16" :title="columnList[4].title" />
    </template>
    <template #body="{ rowData: index }">
      <BBTableCell :left-padding="4">
        {{ index.expression }}
      </BBTableCell>
      <BBTableCell>
        {{ index.position }}
      </BBTableCell>
      <BBTableCell>
        {{ index.unique }}
      </BBTableCell>
      <BBTableCell v-if="showVisibleColumn">
        {{ index.visible }}
      </BBTableCell>
      <BBTableCell>
        {{ index.comment }}
      </BBTableCell>
    </template>
  </BBTable>
</template>

<script lang="ts">
import { computed, defineComponent, PropType } from "vue";
import { BBTableSectionDataSource } from "../bbkit/types";
import { Database, TableIndex } from "../types";
import { useI18n } from "vue-i18n";

export default defineComponent({
  name: "IndexTable",
  components: {},
  props: {
    indexList: {
      required: true,
      type: Object as PropType<TableIndex[]>,
    },
    database: {
      required: true,
      type: Object as PropType<Database>,
    },
  },
  setup(props) {
    const { t } = useI18n();
    const showVisibleColumn = computed(() => {
      return props.database.instance.engine !== "POSTGRES";
    });
    const columnList = computed(() => [
      {
        title: t("database.expression"),
      },
      {
        title: t("database.position"),
      },
      {
        title: t("database.unique"),
      },
      {
        title: t("database.visible"),
      },
      {
        title: t("database.comment"),
      },
    ]);
    const sectionList = computed(() => {
      const sectionList: BBTableSectionDataSource<TableIndex>[] = [];

      for (const index of props.indexList) {
        const item = sectionList.find((item) => item.title == index.name);
        if (item) {
          item.list.push(index);
        } else {
          sectionList.push({
            title: index.name,
            list: [index],
          });
        }
      }

      return sectionList;
    });

    return {
      columnList,
      sectionList,
      showVisibleColumn,
    };
  },
});
</script>
