/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React from 'react';
import { Modal, Select, Typography } from '@douyinfe/semi-ui';

const BatchGroupModal = ({
  showBatchGroup,
  setShowBatchGroup,
  batchChannelGroup,
  batchGroupValue,
  setBatchGroupValue,
  batchGroupAction,
  groupOptions,
  selectedChannels,
  t,
}) => {
  const isRemove = batchGroupAction === 'remove';
  return (
    <Modal
      title={isRemove ? t('批量删除分组') : t('批量添加分组')}
      visible={showBatchGroup}
      onOk={batchChannelGroup}
      onCancel={() => setShowBatchGroup(false)}
      maskClosable={false}
      centered={true}
      size='small'
      className='!rounded-lg'
    >
      <div className='mb-3'>
        <Typography.Text>
          {isRemove
            ? t('选择要从所选渠道移除的分组')
            : t('选择要为所选渠道添加的分组')}
        </Typography.Text>
      </div>
      <Select
        placeholder={t('请选择分组')}
        optionList={groupOptions}
        value={batchGroupValue}
        onChange={(v) => setBatchGroupValue(v)}
        filter
        allowCreate
        style={{ width: '100%' }}
      />
      <div className='mt-4'>
        <Typography.Text type='secondary'>
          {t('已选择 ${count} 个渠道').replace(
            '${count}',
            selectedChannels.length,
          )}
        </Typography.Text>
      </div>
    </Modal>
  );
};

export default BatchGroupModal;
